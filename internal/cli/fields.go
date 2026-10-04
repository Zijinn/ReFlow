package cli

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// fieldKind is how a paper field's --set value is read.
type fieldKind uint8

const (
	// fieldString takes the value literally.
	fieldString fieldKind = iota
	// fieldInt takes a decimal number.
	fieldInt
	// fieldNullableInt takes a decimal number or the literal `null`, which clears
	// the field: the PATCH handler distinguishes "absent" from "explicit null" only
	// for this column.
	fieldNullableInt
	// fieldList accepts a JSON array, or a comma-separated list for the common case.
	fieldList
	// fieldJSON accepts exactly one JSON value, kept verbatim.
	fieldJSON
)

// paperFields mirrors the PATCH /api/v1/research/papers/{id} body one-for-one.
//
// The handler decodes into a struct of pointers, and Go drops JSON keys it does
// not recognise: a misspelled field would be accepted with a 200 while writing
// nothing. That is the worst possible failure for an agent, so the table below is
// checked before the request leaves the CLI.
var paperFields = map[string]fieldKind{
	"title":            fieldString,
	"authors":          fieldList,
	"keywords":         fieldList,
	"file_path":        fieldString,
	"next_action":      fieldString,
	"notes":            fieldString,
	"research_area":    fieldString,
	"status":           fieldString,
	"tag_ids":          fieldList,
	"target_journal":   fieldString,
	"stages":           fieldJSON,
	"current_journal":  fieldString,
	"submission_date":  fieldString,
	"manuscript_id":    fieldString,
	"submission_count": fieldInt,
	"target_level":     fieldString,
	"editor":           fieldString,
	"deadline":         fieldString,
	"history":          fieldJSON,
	"abstract":         fieldString,
	"journal":          fieldString,
	"language":         fieldString,
	"year":             fieldString,
	"volume":           fieldString,
	"issue":            fieldString,
	"pages":            fieldString,
	"doi":              fieldString,
	"citations":        fieldNullableInt,
	"citation_source":  fieldString,
}

func paperFieldNames() []string {
	out := make([]string, 0, len(paperFields))
	for name := range paperFields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildPatch turns `field=value` assignments into one PATCH body. Later
// assignments to the same field win, which lets `--set notes= --set doi=10.` read
// as a sequence of edits rather than a merge puzzle.
func buildPatch(assignments []string) (map[string]any, error) {
	if len(assignments) == 0 {
		return nil, nil
	}
	patch := make(map[string]any, len(assignments))
	for _, raw := range assignments {
		name, value, found := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, usage(`--set expects field=value, got %q`, raw)
		}
		kind, known := paperFields[name]
		if !known {
			return nil, usage("--set field %q is not a paper field, want one of: %s",
				name, strings.Join(paperFieldNames(), ", "))
		}
		converted, err := convertField(kind, name, value)
		if err != nil {
			return nil, err
		}
		patch[name] = converted
	}
	return patch, nil
}

func convertField(kind fieldKind, name, value string) (any, error) {
	switch kind {
	case fieldInt, fieldNullableInt:
		if kind == fieldNullableInt && strings.EqualFold(strings.TrimSpace(value), "null") {
			return nil, nil
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			if kind == fieldNullableInt {
				return nil, usage("--set %s expects an integer or null, got %q", name, value)
			}
			return nil, usage("--set %s expects an integer, got %q", name, value)
		}
		return parsed, nil
	case fieldList:
		trimmed := strings.TrimSpace(value)
		if strings.HasPrefix(trimmed, "[") {
			var items []string
			if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
				return nil, usage("--set %s expects a JSON array of strings, got %q", name, value)
			}
			return items, nil
		}
		items := make([]string, 0, 4)
		for _, item := range strings.Split(trimmed, ",") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, item)
			}
		}
		return items, nil
	case fieldJSON:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return []any{}, nil
		}
		if !json.Valid([]byte(trimmed)) {
			return nil, usage("--set %s expects a JSON value, got %q", name, value)
		}
		var out any
		if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
			return nil, usage("--set %s: %v", name, err)
		}
		return out, nil
	default:
		return value, nil
	}
}

// fieldHintList renders the field table as `name=<shape>` entries, for
// help text and for `describe`.
func fieldHintList() []string {
	out := make([]string, 0, len(paperFields))
	for _, name := range paperFieldNames() {
		switch paperFields[name] {
		case fieldInt:
			out = append(out, name+"=<int>")
		case fieldNullableInt:
			out = append(out, name+"=<int|null>")
		case fieldList:
			out = append(out, name+"=<a,b|json-array>")
		case fieldJSON:
			out = append(out, name+"=<json>")
		default:
			out = append(out, name+"=<text>")
		}
	}
	return out
}

func fieldHint() string { return strings.Join(fieldHintList(), ", ") }

func requireOneArg(args []string, what string) (string, error) {
	if len(args) != 1 {
		return "", usage("%s needs exactly one argument, got %d", what, len(args))
	}
	return args[0], nil
}
