package cli

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/Zijinn/ReFlow/internal/domain"
)

const (
	researchPapersPath = "/api/v1/research/papers"
	researchTagsPath   = "/api/v1/research/tags"
)

// researchKinds are the workspace buckets: 在研, 在投, 已发表.
var researchKinds = []string{
	domain.ResearchKindResearch,
	domain.ResearchKindSubmitted,
	domain.ResearchKindPublished,
}

func paperCommands() []Command {
	setFlag := Flag{
		Name: "set", Kind: KindStrings,
		Description: "paper field assignment, repeatable: --set doi=10.1234/x --set 'authors=A,B'" +
			" | fields: " + fieldHint(),
	}
	return []Command{
		{
			Path:     "paper list",
			Summary:  "list the papers of one workspace bucket, in table order",
			Endpoint: "GET " + researchPapersPath + "?kind=",
			Flags: []Flag{{
				Name: "kind", Kind: KindChoice, Choices: researchKinds, Required: true,
				Description: "workspace bucket",
			}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw, err := app.client.Get(ctx, researchPapersPath, url.Values{"kind": {v.String("kind")}})
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "paper get",
			Summary:  "read one paper with its stage tree, labels and submission history",
			Args:     "<paper-id>",
			Endpoint: "GET " + researchPapersPath + "/{paperID}",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "paper get")
				if err != nil {
					return err
				}
				raw, err := app.client.Get(ctx, researchPapersPath+"/"+url.PathEscape(id), nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "paper create",
			Summary:  "create a paper in one bucket; --set fields are written by a follow-up PATCH",
			Endpoint: "POST " + researchPapersPath + ", then PATCH " + researchPapersPath + "/{paperID}",
			Flags: []Flag{
				{Name: "kind", Kind: KindChoice, Choices: researchKinds, Required: true,
					Description: "bucket to create in: research (在研), submitted (在投), published (已发表)"},
				{Name: "title", Kind: KindString, Required: true, Description: "paper title"},
				{Name: "author", Kind: KindStrings,
					Description: "author name, repeatable"},
				setFlag,
			},
			Handler: paperCreate,
		},
		{
			Path:     "paper set",
			Summary:  "edit any paper field in place",
			Args:     "<paper-id>",
			Endpoint: "PATCH " + researchPapersPath + "/{paperID}",
			Flags:    []Flag{setFlag},
			Handler:  paperSet,
		},
		{
			Path:     "paper move",
			Summary:  "move a paper between buckets, which is how 在投 becomes 已发表",
			Args:     "<paper-id>",
			Endpoint: "POST " + researchPapersPath + "/{paperID}/move",
			Flags: []Flag{{Name: "kind", Kind: KindChoice, Choices: researchKinds, Required: true,
				Description: "destination bucket"}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "paper move")
				if err != nil {
					return err
				}
				raw, err := app.client.Call(ctx, "POST",
					researchPapersPath+"/"+url.PathEscape(id)+"/move", nil,
					map[string]any{"kind": v.String("kind")})
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "paper delete",
			Summary:  "delete a paper, destructive and gated behind --yes",
			Args:     "<paper-id>",
			Endpoint: "DELETE " + researchPapersPath + "/{paperID}",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "paper delete")
				if err != nil {
					return err
				}
				if err := app.Confirm("paper delete " + id); err != nil {
					return err
				}
				if _, err := app.client.Call(ctx, "DELETE",
					researchPapersPath+"/"+url.PathEscape(id), nil, nil); err != nil {
					return err
				}
				return app.Emit(map[string]any{"deleted": id})
			},
		},
		{
			Path:     "paper citation",
			Summary:  "fill journal, year, volume, pages and citation count from Crossref by DOI",
			Args:     "<paper-id>",
			Endpoint: "POST " + researchPapersPath + "/{paperID}/citation",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "paper citation")
				if err != nil {
					return err
				}
				raw, err := app.client.Call(ctx, "POST",
					researchPapersPath+"/"+url.PathEscape(id)+"/citation", nil, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "paper reorder",
			Summary:  "write a bucket's row order",
			Endpoint: "POST " + researchPapersPath + "/reorder",
			Flags: []Flag{
				{Name: "kind", Kind: KindChoice, Choices: researchKinds, Required: true,
					Description: "bucket to reorder"},
				{Name: "paper-id", Kind: KindStrings, Required: true,
					Description: "every paper id of the bucket, in the new order, repeatable"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if _, err := app.client.Call(ctx, "POST", researchPapersPath+"/reorder", nil, map[string]any{
					"kind": v.String("kind"), "paper_ids": v.Strings("paper-id"),
				}); err != nil {
					return err
				}
				return app.Emit(map[string]any{"reordered": v.String("kind"), "count": len(v.Strings("paper-id"))})
			},
		},
	}
}

func paperCreate(ctx context.Context, app *App, v *Values, args []string) error {
	if len(args) > 0 {
		return usage("paper create takes no positional argument, got %q", args[0])
	}
	// The field list is validated before the row is created: a typo in --set should
	// refuse at exit 2 and leave nothing behind, not half-write a paper.
	patch, err := buildPatch(v.Strings("set"))
	if err != nil {
		return err
	}
	body := map[string]any{"kind": v.String("kind"), "title": v.String("title")}
	if authors := v.Strings("author"); len(authors) > 0 {
		body["authors"] = authors
	}
	created, err := app.client.Call(ctx, "POST", researchPapersPath, nil, body)
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return app.Emit(json.RawMessage(created))
	}
	id, err := paperID(created)
	if err != nil {
		return &PartialError{Err: err, Data: json.RawMessage(created)}
	}
	updated, err := app.client.Call(ctx, "PATCH", researchPapersPath+"/"+url.PathEscape(id), nil, patch)
	if err != nil {
		return &PartialError{
			Err: err,
			Data: map[string]any{
				"paper_id":      id,
				"created":       json.RawMessage(created),
				"fields_undone": patchKeys(patch),
			},
		}
	}
	return app.Emit(json.RawMessage(updated))
}

func paperSet(ctx context.Context, app *App, v *Values, args []string) error {
	id, err := requireOneArg(args, "paper set")
	if err != nil {
		return err
	}
	patch, err := buildPatch(v.Strings("set"))
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		return usage("paper set needs at least one --set field=value")
	}
	raw, err := app.client.Call(ctx, "PATCH", researchPapersPath+"/"+url.PathEscape(id), nil, patch)
	if err != nil {
		return err
	}
	return app.Emit(json.RawMessage(raw))
}

// paperID reads the id out of a create response without decoding the whole paper,
// so a new server-side field can not break the follow-up PATCH.
func paperID(raw json.RawMessage) (string, error) {
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return "", &TransportError{Reason: "create returned a body without a usable id"}
	}
	if created.ID == "" {
		return "", &TransportError{Reason: "create returned a paper with an empty id"}
	}
	return created.ID, nil
}

func patchKeys(patch map[string]any) []string {
	out := make([]string, 0, len(patch))
	for name := range patch {
		out = append(out, name)
	}
	return out
}
