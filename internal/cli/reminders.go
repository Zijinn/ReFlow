package cli

import (
	"context"
	"encoding/json"
	"net/url"
)

const remindersPath = "/api/v1/tasks/reminders"
const remindersSyncPath = "/api/v1/tasks/reminders/sync"

func remindersCommands() []Command {
	return []Command{
		{
			Path:     "reminders status",
			Summary:  "report whether Apple Reminders is reachable and which list is used",
			Endpoint: "GET " + remindersPath,
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if len(args) > 0 {
					return usage("reminders status takes no positional argument, got %q", args[0])
				}
				raw, err := app.client.Get(ctx, remindersPath, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "reminders sync",
			Summary:  "merge 即时任务 with the local Apple Reminders list",
			Endpoint: "POST " + remindersSyncPath,
			Flags: []Flag{
				{Name: "direction", Kind: KindChoice, Choices: []string{"merge", "push", "pull"}, Default: "merge",
					Description: "merge keeps both sides, push sends ReFlow changes, pull imports Reminders changes"},
				{Name: "dry-run", Kind: KindBool,
					Description: "show the plan without writing either side or updating the mapping file"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				return runRemindersSync(ctx, app, v, args, "")
			},
		},
		{
			Path:     "reminders push",
			Summary:  "send ReFlow tasks to Apple Reminders without importing",
			Endpoint: "POST " + remindersSyncPath + "?direction=push",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				return runRemindersSync(ctx, app, v, args, "push")
			},
		},
		{
			Path:     "reminders pull",
			Summary:  "import Apple Reminders tasks without sending ReFlow changes",
			Endpoint: "POST " + remindersSyncPath + "?direction=pull",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				return runRemindersSync(ctx, app, v, args, "pull")
			},
		},
	}
}

func runRemindersSync(ctx context.Context, app *App, v *Values, args []string, forcedDirection string) error {
	if len(args) > 0 {
		return usage("reminders sync takes no positional argument, got %q", args[0])
	}
	direction := forcedDirection
	if direction == "" && v != nil {
		direction = v.String("direction")
	}
	if direction == "" {
		direction = "merge"
	}
	query := url.Values{"direction": []string{direction}}
	if v != nil && v.Bool("dry-run") {
		query.Set("dry_run", "true")
	}
	raw, err := app.client.Call(ctx, "POST", remindersSyncPath, query, nil)
	if err != nil {
		return err
	}
	return app.Emit(json.RawMessage(raw))
}
