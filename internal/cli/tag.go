package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Tag namespaces. The reader labels entries, the workbench labels papers, and the
// two sets are stored apart: `--scope` picks which one a command reads or writes.
const (
	tagScopeResearch = "research"
	tagScopeReader   = "reader"
)

// researchTagColors is the palette wb-badge--<name> ships. A name outside it is
// rejected by the server, so the CLI lists it in help instead of guessing.
var researchTagColors = []string{
	"amber", "blue", "gray", "green", "orange", "red", "teal", "violet",
}

func tagCommands() []Command {
	scope := Flag{
		Name: "scope", Kind: KindChoice, Choices: []string{tagScopeResearch, tagScopeReader},
		Default: tagScopeResearch,
		Description: "research labels papers (在研/在投/已发表), reader labels RSS entries; " +
			"research is a priority-ordered palette, reader is a plain label set",
	}
	return []Command{
		{
			Path:     "tag list",
			Summary:  "list labels in priority order",
			Endpoint: "GET " + researchTagsPath + " | GET /api/v1/tags",
			Flags:    []Flag{scope},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				scope := tagScope(v)
				path := tagPath(scope)
				raw, err := app.client.Get(ctx, path, nil)
				if err != nil {
					return err
				}
				return app.Emit(tagListBody(scope, raw))
			},
		},
		{
			Path:     "tag create",
			Summary:  "create a label; position is the palette order for the research scope",
			Endpoint: "POST " + researchTagsPath + " | POST /api/v1/tags",
			Flags: []Flag{
				scope,
				{Name: "name", Kind: KindString, Required: true, Description: "label name, unique per scope"},
				{Name: "color", Kind: KindString,
					Description: "research: one of " + strings.Join(researchTagColors, ", ") +
						", empty stores no colour; reader: a colour string the reader UI can render"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				scope := tagScope(v)
				body := map[string]any{"name": v.String("name")}
				switch scope {
				case tagScopeResearch:
					body["color"] = v.String("color")
				default:
					// The reader column is nullable: sending "" would store an empty
					// colour instead of no colour.
					if color := v.String("color"); color != "" {
						body["color"] = color
					}
				}
				raw, err := app.client.Call(ctx, "POST", tagPath(scope), nil, body)
				if err != nil {
					return err
				}
				return app.Emit(tagWriteBody(scope, raw))
			},
		},
		{
			Path:     "tag set",
			Summary:  "rename, recolour, or switch a research label's colour painting on and off",
			Args:     "<tag-id>",
			Endpoint: "PATCH " + researchTagsPath + "/{tagID}",
			Flags: []Flag{
				scope,
				{Name: "name", Kind: KindString, Description: "new label name"},
				{Name: "color", Kind: KindString, Description: "new colour, from the research palette"},
				{Name: "color-enabled", Kind: KindChoice, Choices: []string{"on", "off"},
					Description: "research only: paint papers with this label's colour (on) or keep the " +
						"colour on record while rendering the chip neutral (off)"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "tag set")
				if err != nil {
					return err
				}
				scope := tagScope(v)
				if scope != tagScopeResearch {
					return usage("the reader label set has no update route, only create and delete; " +
						"`tag set --scope reader` is not possible, delete and recreate instead")
				}
				body := map[string]any{}
				if name := v.String("name"); name != "" {
					body["name"] = name
				}
				if color := v.String("color"); color != "" {
					body["color"] = color
				}
				switch v.String("color-enabled") {
				case "on":
					body["color_enabled"] = true
				case "off":
					body["color_enabled"] = false
				}
				if len(body) == 0 {
					return usage("tag set needs at least one of --name, --color, --color-enabled")
				}
				raw, err := app.client.Call(ctx, "PATCH",
					researchTagsPath+"/"+url.PathEscape(id), nil, body)
				if err != nil {
					return err
				}
				return app.Emit(tagWriteBody(scope, raw))
			},
		},
		{
			Path:     "tag delete",
			Summary:  "delete a label, destructive and gated behind --yes",
			Args:     "<tag-id>",
			Endpoint: "DELETE " + researchTagsPath + "/{tagID} | DELETE /api/v1/tags/{tagID}",
			Flags:    []Flag{scope},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "tag delete")
				if err != nil {
					return err
				}
				scope := tagScope(v)
				if err := app.Confirm("tag delete " + id); err != nil {
					return err
				}
				if _, err := app.client.Call(ctx, "DELETE",
					tagPath(scope)+"/"+url.PathEscape(id), nil, nil); err != nil {
					return err
				}
				return app.Emit(map[string]any{"deleted": id, "scope": scope})
			},
		},
		{
			Path:     "tag reorder",
			Summary:  "write the research palette order, which is also the priority order",
			Endpoint: "POST " + researchTagsPath + "/reorder",
			Flags: []Flag{
				scope,
				{Name: "tag-id", Kind: KindStrings, Required: true,
					Description: "every research label id, in the new order, repeatable"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if tagScope(v) != tagScopeResearch {
					return usage("tag reorder only exists for the research palette")
				}
				if _, err := app.client.Call(ctx, "POST", researchTagsPath+"/reorder", nil,
					map[string]any{"tag_ids": v.Strings("tag-id")}); err != nil {
					return err
				}
				return app.Emit(map[string]any{"reordered": len(v.Strings("tag-id"))})
			},
		},
	}
}

func tagScope(v *Values) string {
	if scope := v.String("scope"); scope != "" {
		return scope
	}
	return tagScopeResearch
}

func tagPath(scope string) string {
	if scope == tagScopeReader {
		return "/api/v1/tags"
	}
	return researchTagsPath
}

// The two namespaces answer differently: the research routes wrap in `tags` / `tag`,
// the reader route answers `items` and a bare object. A caller asking "which labels
// exist" or "what id did I just create" should not have to know which scope it
// queried, so both verbs answer one shape and carry the scope they read.
func tagListBody(scope string, raw json.RawMessage) any {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return json.RawMessage(raw)
	}
	items, ok := body["items"]
	if !ok {
		if items, ok = body["tags"]; !ok {
			return json.RawMessage(raw)
		}
	}
	return map[string]any{"scope": scope, "items": items}
}

func tagWriteBody(scope string, raw json.RawMessage) any {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err == nil {
		if tag, ok := body["tag"]; ok {
			return map[string]any{"scope": scope, "tag": tag}
		}
	}
	return map[string]any{"scope": scope, "tag": raw}
}
