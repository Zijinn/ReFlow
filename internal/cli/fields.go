package cli

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// fieldKind is how a --set value is read.
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
	// fieldNullableString takes text, or the literal `null` to hand the value back.
	// A subscription's folder and its title override are the two columns whose PATCH
	// body distinguishes "absent" from "explicit null": clearing one is the edit an
	// agent wants, and there is no other value that means it.
	fieldNullableString
	// fieldBool takes `true` or `false`. An empty value is a mistake, not a false.
	fieldBool
)

// patchTable is one endpoint's set of writable fields, keyed by the JSON name the
// PATCH handler decodes.
type patchTable map[string]fieldKind

// paperFields mirrors the PATCH /api/v1/research/papers/{id} body one-for-one.
//
// The handler decodes into a struct of pointers, and Go drops JSON keys it does
// not recognise: a misspelled field would be accepted with a 200 while writing
// nothing. That is the worst possible failure for an agent, so the table below is
// checked before the request leaves the CLI.
var paperFields = patchTable{
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

// feedFields mirrors PATCH /api/v1/feeds/{feedID}, which writes the subscription
// row rather than the feed document. `url` is absent on purpose: the server never
// repoints a subscription, so a feed whose address changed is an add plus a delete.
// view_mode and refresh_policy are left as plain strings — the handler checks them
// against a fixed set and answers with the list it wanted, which the caller reads
// more usefully than a second copy of that list in here.
var feedFields = patchTable{
	"folder_id":                fieldNullableString,
	"title_override":           fieldNullableString,
	"view_mode":                fieldString,
	"refresh_policy":           fieldString,
	"refresh_interval_minutes": fieldInt,
	"hide_from_timeline":       fieldBool,
	"position":                 fieldInt,
}

// folderFields mirrors PATCH /api/v1/folders/{folderID}.
var folderFields = patchTable{
	"name":      fieldString,
	"parent_id": fieldNullableString,
	"position":  fieldInt,
}

// tableFieldNames lists one table's writable fields, sorted for help text and errors.
func tableFieldNames(table patchTable) []string {
	out := make([]string, 0, len(table))
	for name := range table {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildPatch turns `field=value` assignments into one PATCH body against `table`.
// `noun` names the thing being edited, because a caller that typed
// `feed set --set title=...` needs to be told a feed has no such field rather than
// reading a list of paper columns. Later assignments to the same field win, which
// lets `--set notes= --set doi=10.` read as a sequence of edits rather than a merge
// puzzle.
func buildPatch(table patchTable, noun string, assignments []string) (map[string]any, error) {
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
		kind, known := table[name]
		if !known {
			return nil, usage("--set field %q is not a %s field, want one of: %s",
				name, noun, strings.Join(tableFieldNames(table), ", "))
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
	case fieldNullableString:
		if strings.EqualFold(strings.TrimSpace(value), "null") {
			return nil, nil
		}
		return value, nil
	case fieldBool:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, usage("--set %s expects true or false, got %q", name, value)
		}
		return parsed, nil
	default:
		return value, nil
	}
}

// patchHintList renders one field table as `name=<shape>` entries, for help text
// and for `describe`.
func patchHintList(table patchTable) []string {
	out := make([]string, 0, len(table))
	for _, name := range tableFieldNames(table) {
		switch table[name] {
		case fieldInt:
			out = append(out, name+"=<int>")
		case fieldNullableInt:
			out = append(out, name+"=<int|null>")
		case fieldList:
			out = append(out, name+"=<a,b|json-array>")
		case fieldJSON:
			out = append(out, name+"=<json>")
		case fieldNullableString:
			out = append(out, name+"=<text|null>")
		case fieldBool:
			out = append(out, name+"=<true|false>")
		default:
			out = append(out, name+"=<text>")
		}
	}
	return out
}

// patchHint renders one field table as the `--set` field list a caller reads in
// help and in `describe`.
func patchHint(table patchTable) string { return strings.Join(patchHintList(table), ", ") }

func requireOneArg(args []string, what string) (string, error) {
	if len(args) != 1 {
		return "", usage("%s needs exactly one argument, got %d", what, len(args))
	}
	return args[0], nil
}
