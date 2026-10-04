// Package cli implements the `reflow` command: a JSON-first client for ReFlow
// Server's HTTP API, shaped so an AI agent can create and edit workspace papers
// and tags, and turn RSS entries into tracked literature.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Exit codes are contractual: an agent branches on these instead of reading
// prose. They never overlap with the HTTP status a command reports inside the
// error object.
const (
	ExitOK        = 0
	ExitUsage     = 2
	ExitAPI       = 3
	ExitTransport = 4
)

const (
	FormatJSON   = "json"
	FormatPretty = "pretty"
)

// Kind is a flag's value type. It doubles as the type an agent sees in
// `reflow describe`, so the catalog and the parser share one definition.
type Kind string

const (
	KindString  Kind = "string"
	KindStrings Kind = "string[]"
	KindBool    Kind = "bool"
	KindInt     Kind = "int"
	KindChoice  Kind = "choice"
)

// Flag describes one option of a command.
type Flag struct {
	Name        string
	Kind        Kind
	Choices     []string
	Default     string
	Required    bool
	Description string
}

// Command is one leaf of the tree: either "<group> <verb>" or a bare verb.
// Endpoint records which API call it makes, so `describe` can show an agent what
// sits behind a command without reading source.
type Command struct {
	Path     string
	Summary  string
	Args     string
	Endpoint string
	Flags    []Flag
	Handler  func(ctx context.Context, app *App, v *Values, args []string) error
}

// UsageError is caller-fixable: a missing flag, a typo in a field name, a
// destructive command run without --yes.
type UsageError struct{ Reason string }

func (e *UsageError) Error() string { return e.Reason }

func usage(format string, args ...any) error {
	return &UsageError{Reason: fmt.Sprintf(format, args...)}
}

// PartialError is a failure that still left a durable row behind: a create whose
// follow-up PATCH was rejected. It carries the created object so the caller can
// see what exists instead of guessing whether the write landed.
type PartialError struct {
	Err  error
	Data any
}

func (e *PartialError) Error() string { return e.Err.Error() }

func (e *PartialError) Unwrap() error { return e.Err }

// App is the per-invocation environment a command handler runs in.
type App struct {
	client *Client
	stdout io.Writer
	format string
	yes    bool
	now    func() time.Time
}

// Emit writes one success envelope. Every command answers with the same shape, so
// a caller can parse stdout unconditionally, including when the command failed.
func (a *App) Emit(data any) error {
	return writeJSON(a.stdout, envelope{OK: true, Data: data}, a.format == FormatPretty)
}

// Confirm refuses a destructive command unless the caller passed --yes. Deleting
// a row is the one thing an agent should not do on its own initiative, so the
// refusal is a machine-readable exit code rather than a wall of text.
func (a *App) Confirm(action string) error {
	if a.yes {
		return nil
	}
	return usage("%s refused: pass --yes to confirm", action)
}

type envelope struct {
	OK    bool     `json:"ok"`
	Data  any      `json:"data,omitempty"`
	Error *payload `json:"error,omitempty"`
}

type payload struct {
	Class     string `json:"class"`
	Message   string `json:"message"`
	Code      string `json:"code,omitempty"`
	Status    int    `json:"status,omitempty"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

type globals struct {
	url     string
	token   string
	format  string
	timeout time.Duration
	yes     bool
	help    bool
}

// Run parses args, executes one command, and returns the process exit code.
// stdout carries exactly one JSON document; help text and the flag package's own
// diagnostics go to stderr.
func Run(ctx context.Context, args []string, commands []Command, stdout, stderr io.Writer) int {
	table := newRegistry(commands)
	opts, rest, err := extractGlobals(args)
	if err != nil {
		return fail(stdout, FormatJSON, err)
	}
	if opts.format != FormatJSON && opts.format != FormatPretty {
		return fail(stdout, FormatJSON, usage("unknown --format %q, want json or pretty", opts.format))
	}
	if len(rest) == 0 {
		printRootHelp(stderr, table)
		return fail(stdout, opts.format, usage("no command given"))
	}
	if rest[0] == "help" {
		return runHelp(stdout, opts.format, table, rest[1:])
	}
	command, positional, err := table.resolve(rest)
	if err != nil {
		return fail(stdout, opts.format, err)
	}
	set := flag.NewFlagSet(command.Path, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.Usage = func() {}
	values, err := bindFlags(set, command.Flags)
	if err != nil {
		return fail(stdout, opts.format, err)
	}
	registerGlobals(set, opts)
	options, positional, err := reorderOptions(set, positional)
	if err != nil {
		return fail(stdout, opts.format, err)
	}
	if err := set.Parse(append(options, positional...)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandHelp(stdout, command)
			return ExitOK
		}
		return fail(stdout, opts.format, usage("%s", err.Error()))
	}
	if opts.help {
		printCommandHelp(stdout, command)
		return ExitOK
	}
	if err := validateFlags(command, values); err != nil {
		return fail(stdout, opts.format, err)
	}
	app := &App{
		client: newClient(opts),
		stdout: stdout,
		format: opts.format,
		yes:    opts.yes,
		now:    time.Now,
	}
	if err := command.Handler(ctx, app, values, set.Args()); err != nil {
		return fail(stdout, opts.format, err)
	}
	return ExitOK
}

// fail writes the error envelope and maps the error onto its exit class.
func fail(stdout io.Writer, format string, err error) int {
	var data any
	var partial *PartialError
	if errors.As(err, &partial) {
		data, err = partial.Data, partial.Err
	}
	var misuse *UsageError
	var rejected *APIError
	body := envelope{OK: false, Data: data}
	switch {
	case errors.As(err, &misuse):
		body.Error = &payload{Class: "usage", Message: misuse.Reason}
		_ = writeJSON(stdout, body, format == FormatPretty)
		return ExitUsage
	case errors.As(err, &rejected):
		body.Error = &payload{
			Class: "api", Message: rejected.Error(), Code: rejected.Code, Status: rejected.Status,
			Detail: rejected.Detail, RequestID: rejected.RequestID,
			Method: rejected.Method, Path: rejected.Path, Hint: rejected.Hint,
		}
		_ = writeJSON(stdout, body, format == FormatPretty)
		return ExitAPI
	default:
		body.Error = &payload{Class: "transport", Message: err.Error()}
		_ = writeJSON(stdout, body, format == FormatPretty)
		return ExitTransport
	}
}

func writeJSON(w io.Writer, value any, pretty bool) error {
	encoder := json.NewEncoder(w)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

// extractGlobals consumes the global options written before the command path. The
// same options may follow the path, where registerGlobals picks them up.
func extractGlobals(args []string) (*globals, []string, error) {
	opts := &globals{
		url:    defaultBaseURL(),
		format: FormatJSON,
		// A metadata fill or a Crossref lookup is a network call nobody is watching,
		// so every request is bounded.
		timeout: 30 * time.Second,
	}
	if raw := strings.TrimSpace(readEnv("REFLOW_URL")); raw != "" {
		opts.url = raw
	}
	if raw := strings.TrimSpace(readEnv("REFLOW_TOKEN")); raw != "" {
		opts.token = raw
	}
	index := 0
loop:
	for index < len(args) {
		token := args[index]
		if !strings.HasPrefix(token, "-") {
			break
		}
		name, inline, hasInline := strings.Cut(strings.TrimLeft(token, "-"), "=")
		switch name {
		case "url", "token", "format", "timeout":
			value, next, err := leadingValue(args, index, name, inline, hasInline)
			if err != nil {
				return nil, nil, err
			}
			switch name {
			case "url":
				opts.url = value
			case "token":
				opts.token = value
			case "format":
				opts.format = value
			default:
				parsed, err := time.ParseDuration(value)
				if err != nil {
					return nil, nil, usage("invalid --timeout: %v", err)
				}
				opts.timeout = parsed
			}
			index = next
		case "yes":
			opts.yes = true
			index++
		case "help", "h":
			opts.help = true
			index++
		default:
			// Not a global: everything from here belongs to the command, whose own
			// flag set reports an unknown option as a usage error.
			break loop
		}
	}
	return opts, args[index:], nil
}

func leadingValue(args []string, index int, name, inline string, hasInline bool) (string, int, error) {
	if hasInline {
		return inline, index + 1, nil
	}
	if index+1 >= len(args) {
		return "", index, usage("--%s needs a value", name)
	}
	return args[index+1], index + 2, nil
}

// registerGlobals mirrors the global options onto a command's flag set, so
// `reflow paper list --url ...` behaves like the leading position.
func registerGlobals(set *flag.FlagSet, opts *globals) {
	set.StringVar(&opts.url, "url", opts.url, "server base URL")
	set.StringVar(&opts.token, "token", opts.token, "device bearer token")
	set.StringVar(&opts.format, "format", opts.format, "output format")
	set.DurationVar(&opts.timeout, "timeout", opts.timeout, "per-request timeout")
	set.BoolVar(&opts.yes, "yes", opts.yes, "confirm a destructive command")
	set.BoolVar(&opts.help, "help", opts.help, "show help")
	set.BoolVar(&opts.help, "h", opts.help, "show help")
}

// reorderOptions lifts options out of an argument list so they arrive ahead of the
// positionals. The flag package stops reading options at the first positional
// argument, which would read `paper set p-1 --set doi=10.` as three positionals,
// and both a human and an agent write the id first.
func reorderOptions(set *flag.FlagSet, args []string) ([]string, []string, error) {
	options := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		token := args[index]
		if token == "--" {
			// The terminator is kept and moved with the options: it is what lets an id
			// that starts with a dash stay an id instead of becoming a flag.
			positionals = append(positionals, args[index+1:]...)
			options = append(options, token)
			break
		}
		if !strings.HasPrefix(token, "-") || len(token) == 1 {
			positionals = append(positionals, token)
			continue
		}
		options = append(options, token)
		name, _, hasInline := strings.Cut(strings.TrimLeft(token, "-"), "=")
		if hasInline {
			continue
		}
		target := set.Lookup(name)
		if target == nil {
			// Unknown option: pass it through so the flag package reports it, instead
			// of guessing here whether it wants a value.
			continue
		}
		if boolean, ok := target.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			continue
		}
		if index+1 >= len(args) {
			return nil, nil, usage("--%s needs a value", name)
		}
		index++
		options = append(options, args[index])
	}
	return options, positionals, nil
}

// bindFlags builds a command's flag set from its registry entry: one table drives
// parsing, help text, and `describe`, so none of the three can drift apart.
func bindFlags(set *flag.FlagSet, flags []Flag) (*Values, error) {
	values := &Values{
		set:     set,
		strings: map[string]*string{},
		slices:  map[string]*stringSlice{},
		bools:   map[string]*bool{},
		ints:    map[string]*int{},
	}
	for _, item := range flags {
		switch item.Kind {
		case KindString, KindChoice:
			target := new(string)
			set.StringVar(target, item.Name, "", item.Description)
			values.strings[item.Name] = target
		case KindStrings:
			target := new(stringSlice)
			set.Var(target, item.Name, item.Description)
			values.slices[item.Name] = target
		case KindBool:
			target := new(bool)
			set.BoolVar(target, item.Name, false, item.Description)
			values.bools[item.Name] = target
		case KindInt:
			target := new(int)
			set.IntVar(target, item.Name, 0, item.Description)
			values.ints[item.Name] = target
		default:
			return nil, usage("internal error: command flag --%s has unknown kind %q", item.Name, item.Kind)
		}
	}
	return values, nil
}

// validateFlags enforces what the flag package cannot: required options, and
// membership in a choice list.
func validateFlags(command Command, values *Values) error {
	for _, item := range command.Flags {
		switch item.Kind {
		case KindString, KindChoice:
			current := values.String(item.Name)
			if item.Required && current == "" {
				return usage("--%s is required", item.Name)
			}
			if current != "" && len(item.Choices) > 0 && !contains(item.Choices, current) {
				return usage("--%s must be one of %s, got %q",
					item.Name, strings.Join(item.Choices, ", "), current)
			}
		case KindStrings:
			if item.Required && len(values.Strings(item.Name)) == 0 {
				return usage("--%s must be given at least once", item.Name)
			}
		}
	}
	return nil
}

// Values reads the flags a command declared, by name.
type Values struct {
	set     *flag.FlagSet
	strings map[string]*string
	slices  map[string]*stringSlice
	bools   map[string]*bool
	ints    map[string]*int
}

// Changed reports whether the caller wrote this option at all. An int flag left
// out and an int flag given as 0 arrive at the same zero value, and for a bounded
// query parameter they must not answer the same thing.
func (v *Values) Changed(name string) bool {
	changed := false
	v.set.Visit(func(item *flag.Flag) {
		if item.Name == name {
			changed = true
		}
	})
	return changed
}

func (v *Values) String(name string) string {
	if target := v.strings[name]; target != nil {
		return *target
	}
	return ""
}

func (v *Values) Strings(name string) []string {
	if target := v.slices[name]; target != nil {
		return append([]string(nil), (*target)...)
	}
	return nil
}

func (v *Values) Bool(name string) bool {
	if target := v.bools[name]; target != nil {
		return *target
	}
	return false
}

func (v *Values) Int(name string) int {
	if target := v.ints[name]; target != nil {
		return *target
	}
	return 0
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringSlice) Get() any { return []string(*s) }

// registry

type registry struct {
	byPath map[string]Command
	order  []Command
}

func newRegistry(commands []Command) *registry {
	out := &registry{
		byPath: make(map[string]Command, len(commands)),
		order:  append([]Command(nil), commands...),
	}
	sort.SliceStable(out.order, func(i, j int) bool { return out.order[i].Path < out.order[j].Path })
	for _, command := range out.order {
		out.byPath[command.Path] = command
	}
	return out
}

// resolve matches the leading argv tokens against a command path and returns what
// is left for that command's flag set.
func (r *registry) resolve(args []string) (Command, []string, error) {
	if len(args) == 0 {
		return Command{}, nil, usage("no command given")
	}
	if command, ok := r.byPath[args[0]]; ok {
		return command, args[1:], nil
	}
	if len(args) > 1 {
		if command, ok := r.byPath[args[0]+" "+args[1]]; ok {
			return command, args[2:], nil
		}
	}
	if verbs := r.verbs(args[0]); len(verbs) > 0 {
		if len(args) == 1 {
			return Command{}, nil, usage("command `%s` needs a verb, want one of: %s",
				args[0], strings.Join(verbs, ", "))
		}
		return Command{}, nil, usage("unknown subcommand `%s %s`, want one of: %s",
			args[0], args[1], strings.Join(verbs, ", "))
	}
	return Command{}, nil, usage("unknown command %q, run `reflow help` for the command list "+
		"or `reflow describe` for the machine-readable catalog", args[0])
}

func (r *registry) verbs(group string) []string {
	prefix := group + " "
	out := make([]string, 0)
	for _, command := range r.order {
		if name, ok := strings.CutPrefix(command.Path, prefix); ok {
			out = append(out, name)
		}
	}
	return out
}

// help

// runHelp answers `reflow help [<group> <verb>]`. An agent that has not read the
// catalog yet reaches for this form before `describe`, so it is a real command
// rather than a message that points at one.
func runHelp(stdout io.Writer, format string, table *registry, args []string) int {
	if len(args) == 0 {
		printRootHelp(stdout, table)
		return ExitOK
	}
	command, _, err := table.resolve(args)
	if err != nil {
		return fail(stdout, format, err)
	}
	printCommandHelp(stdout, command)
	return ExitOK
}

func printRootHelp(w io.Writer, r *registry) {
	fmt.Fprint(w, "reflow - ReFlow Server command line client\n\nCommands:\n")
	for _, command := range r.order {
		fmt.Fprintf(w, "  reflow %s%s\n      %s\n", command.Path, argsHint(command), command.Summary)
	}
	fmt.Fprint(w, "\nGlobal options: --url, --token, --format json|pretty, --timeout, --yes, --help\n")
	fmt.Fprint(w, "Exit codes: 0 ok, 2 usage, 3 rejected by the server, 4 transport.\n")
	fmt.Fprint(w, "One command's options: reflow help <group> <verb>\n")
	fmt.Fprint(w, "Machine-readable catalog: reflow describe\n")
}

func printCommandHelp(w io.Writer, command Command) {
	fmt.Fprintf(w, "reflow %s%s - %s\n\n", command.Path, argsHint(command), command.Summary)
	if command.Endpoint != "" {
		fmt.Fprintf(w, "Endpoint: %s\n\n", command.Endpoint)
	}
	if len(command.Flags) == 0 {
		fmt.Fprint(w, "No options.\n")
		return
	}
	fmt.Fprint(w, "Options:\n")
	for _, item := range command.Flags {
		suffix := ""
		if len(item.Choices) > 0 {
			suffix += " [" + strings.Join(item.Choices, "|") + "]"
		}
		if item.Default != "" {
			suffix += " (default " + item.Default + ")"
		}
		if item.Required {
			suffix += " (required)"
		}
		fmt.Fprintf(w, "  --%s <%s>%s\n      %s\n", item.Name, item.Kind, suffix, item.Description)
	}
}

func argsHint(command Command) string {
	if command.Args == "" {
		return ""
	}
	return " " + command.Args
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
