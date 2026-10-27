package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

const quickTasksPath = "/api/v1/tasks/quick"

func taskCommands() []Command {
	return []Command{
		{
			Path:     "task list",
			Summary:  "list 即时任务, open ones first and completed ones after",
			Endpoint: "GET " + quickTasksPath,
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if len(args) > 0 {
					return usage("task list takes no positional argument, got %q", args[0])
				}
				raw, err := app.client.Get(ctx, quickTasksPath, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "task create",
			Summary:  "create a flat 即时任务 with optional tags, dates, notes, and a paper link",
			Endpoint: "POST " + quickTasksPath,
			Flags: []Flag{
				{Name: "title", Kind: KindString, Required: true, Description: "task name"},
				{Name: "notes", Kind: KindString, Description: "free-form notes"},
				{Name: "tag", Kind: KindStrings, Description: "task tag; repeat for more than one"},
				{Name: "start", Kind: KindString, Description: "start date, YYYY-MM-DD"},
				{Name: "due", Kind: KindString, Description: "due date, YYYY-MM-DD"},
				{Name: "paper-id", Kind: KindString, Description: "link the task to a research paper"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if len(args) > 0 {
					return usage("task create takes no positional argument, got %q", args[0])
				}
				body := map[string]any{"title": v.String("title")}
				if v.Changed("notes") {
					body["notes"] = v.String("notes")
				}
				if tags := v.Strings("tag"); len(tags) > 0 {
					body["tags"] = tags
				}
				if v.Changed("start") {
					body["start_date"] = v.String("start")
				}
				if v.Changed("due") {
					body["due_date"] = v.String("due")
				}
				if paperID := strings.TrimSpace(v.String("paper-id")); paperID != "" {
					body["paper_id"] = paperID
				}
				raw, err := app.client.Call(ctx, "POST", quickTasksPath, nil, body)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "task set",
			Summary:  "edit a 即时任务; --completed=false reopens it, --paper-id= unlinks a paper",
			Args:     "<task-id>",
			Endpoint: "PATCH " + quickTasksPath + "/{taskID}",
			Flags: []Flag{
				{Name: "title", Kind: KindString, Description: "new task name"},
				{Name: "notes", Kind: KindString, Description: "new notes"},
				{Name: "tag", Kind: KindStrings, Description: "replace tags with this repeatable list"},
				{Name: "clear-tags", Kind: KindBool, Description: "remove every tag; cannot be combined with --tag"},
				{Name: "start", Kind: KindString, Description: "start date, YYYY-MM-DD, or empty to clear"},
				{Name: "due", Kind: KindString, Description: "due date, YYYY-MM-DD, or empty to clear"},
				{Name: "paper-id", Kind: KindString, Description: "replace the paper link; empty unlinks it"},
				{Name: "completed", Kind: KindBool, Description: "true completes, false reopens"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "task set")
				if err != nil {
					return err
				}
				body := map[string]any{}
				if v.Changed("title") {
					body["title"] = v.String("title")
				}
				if v.Changed("notes") {
					body["notes"] = v.String("notes")
				}
				if v.Changed("clear-tags") && v.Bool("clear-tags") && v.Changed("tag") {
					return usage("task set accepts either --tag or --clear-tags, not both")
				}
				if v.Changed("clear-tags") && v.Bool("clear-tags") {
					body["tags"] = []string{}
				} else if v.Changed("tag") {
					body["tags"] = v.Strings("tag")
				}
				if v.Changed("start") {
					body["start_date"] = v.String("start")
				}
				if v.Changed("due") {
					body["due_date"] = v.String("due")
				}
				if v.Changed("paper-id") {
					trimmed := strings.TrimSpace(v.String("paper-id"))
					if trimmed == "" {
						body["paper_id"] = nil
					} else {
						body["paper_id"] = trimmed
					}
				}
				if v.Changed("completed") {
					body["completed"] = v.Bool("completed")
				}
				if len(body) == 0 {
					return usage("task set needs at least one field to change")
				}
				raw, err := app.client.Call(ctx, "PATCH", quickTasksPath+"/"+url.PathEscape(id), nil, body)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "task delete",
			Summary:  "delete a 即时任务, destructive and gated behind --yes",
			Args:     "<task-id>",
			Endpoint: "DELETE " + quickTasksPath + "/{taskID}",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "task delete")
				if err != nil {
					return err
				}
				if err := app.Confirm("task delete"); err != nil {
					return err
				}
				if _, err := app.client.Call(ctx, "DELETE", quickTasksPath+"/"+url.PathEscape(id), nil, nil); err != nil {
					return err
				}
				return app.Emit(map[string]any{"deleted": id})
			},
		},
	}
}
