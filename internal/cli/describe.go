package cli

import (
	"context"
	"fmt"
)

// describeCommand is the machine-readable catalog of the whole CLI. An agent is
// pointed at it instead of at prose: one call yields every command, flag, type and
// endpoint, generated from the same table that parses argv.
func describeCommand() Command {
	return Command{
		Path:     "describe",
		Summary:  "print the machine-readable command catalog for an AI agent",
		Endpoint: "no request",
		Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
			return app.Emit(catalog(AllCommands()))
		},
	}
}

type catalogSpec struct {
	Client        string            `json:"client"`
	APIPrefix     string            `json:"api_prefix"`
	Defaults      catalogDefaults   `json:"defaults"`
	Output        catalogOutput     `json:"output"`
	ExitCodes     map[string]string `json:"exit_codes"`
	Auth          catalogAuth       `json:"auth"`
	Globals       []Flag            `json:"globals"`
	PaperFields   []string          `json:"paper_fields"`
	TagScopes     map[string]string `json:"tag_scopes"`
	ResearchKinds []string          `json:"research_kinds"`
	Commands      []catalogCommand  `json:"commands"`
}

type catalogDefaults struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Timeout string `json:"timeout"`
}

type catalogOutput struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Shape  string `json:"shape"`
}

type catalogAuth struct {
	Loopback    string `json:"loopback"`
	OffMachine  string `json:"off_machine"`
	TokenSource string `json:"token_source"`
}

type catalogCommand struct {
	Path     string `json:"path"`
	Summary  string `json:"summary"`
	Args     string `json:"args,omitempty"`
	Endpoint string `json:"endpoint"`
	Flags    []Flag `json:"flags,omitempty"`
}

func catalog(commands []Command) catalogSpec {
	out := catalogSpec{
		Client:    "reflow",
		APIPrefix: "/api/v1",
		Defaults: catalogDefaults{
			URL: defaultBaseURL(), Format: FormatJSON, Timeout: "30s",
		},
		Output: catalogOutput{
			Stdout: "exactly one JSON document, for successes and failures alike",
			Stderr: "help text and flag-parser diagnostics",
			Shape:  `{"ok":true,"data":...} | {"ok":false,"data":?,"error":{"class","message","code","status","detail","request_id","hint"}}`,
		},
		ExitCodes: map[string]string{
			fmt.Sprint(ExitOK):        "ok",
			fmt.Sprint(ExitUsage):     "bad flags, unknown field, or a destructive command without --yes",
			fmt.Sprint(ExitAPI):       "the server answered 4xx or 5xx; error.code is its problem code",
			fmt.Sprint(ExitTransport): "no server answer: unreachable, timed out, or not JSON",
		},
		Auth: catalogAuth{
			Loopback:    "a call from the same machine over 127.0.0.1 needs no token: the server treats loopback as its own traffic",
			OffMachine:  "against REFLOW_LAN_MODE=true, run `reflow device pairing-code` on the host, then `reflow device pair` and pass the token",
			TokenSource: "--token or REFLOW_TOKEN",
		},
		Globals: []Flag{
			{Name: "url", Kind: KindString, Default: defaultBaseURL(),
				Description: "server base URL, or REFLOW_URL"},
			{Name: "token", Kind: KindString, Description: "device bearer token, or REFLOW_TOKEN"},
			{Name: "format", Kind: KindChoice, Choices: []string{FormatJSON, FormatPretty}, Default: FormatJSON,
				Description: "json is one line; pretty is indented and the same shape"},
			{Name: "timeout", Kind: KindString, Default: "30s", Description: "per-request timeout"},
			{Name: "yes", Kind: KindBool,
				Description: "confirm a destructive command; without it delete answers exit 2"},
		},
		PaperFields:   patchHintList(paperFields),
		ResearchKinds: researchKinds,
		TagScopes: map[string]string{
			tagScopeResearch: "workbench labels; ordered palette, doubles as priority; create/update/reorder all exist",
			tagScopeReader:   "reader labels on RSS entries; create and delete only, no update or reorder route",
		},
	}
	for _, command := range commands {
		out.Commands = append(out.Commands, catalogCommand{
			Path: command.Path, Summary: command.Summary, Args: command.Args,
			Endpoint: command.Endpoint, Flags: command.Flags,
		})
	}
	return out
}
