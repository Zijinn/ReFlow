package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
)

// stdin is what `ai fill --raw -` reads. A variable so a test can feed it.
var stdin io.Reader = os.Stdin

const jobsPath = "/api/v1/jobs"

func statusCommands() []Command {
	return []Command{
		{
			Path:     "status",
			Summary:  "ask the server whether it is up, and what it will accept from this caller",
			Endpoint: "GET /api/v1/status",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw, err := app.client.Get(ctx, "/api/v1/status", nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "job get",
			Summary:  "read one job's state and progress, which is how a queued feed refresh or OPML import is followed",
			Args:     "<job-id>",
			Endpoint: "GET /api/v1/jobs/{jobID}",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "job get")
				if err != nil {
					return err
				}
				raw, err := app.client.Get(ctx, jobsPath+"/"+url.PathEscape(id), nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "job cancel",
			Summary:  "drop a job that is still queued or running; anything finished answers 409 job_not_cancellable",
			Args:     "<job-id>",
			Endpoint: "POST /api/v1/jobs/{jobID}/cancel",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "job cancel")
				if err != nil {
					return err
				}
				raw, err := app.client.Call(ctx, "POST", jobsPath+"/"+url.PathEscape(id)+"/cancel", nil, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "device pairing-code",
			Summary:  "mint a ten-minute pairing code, which only a loopback caller may ask for",
			Endpoint: "POST /api/v1/devices/pairing-code",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw, err := app.client.Call(ctx, "POST", "/api/v1/devices/pairing-code", nil, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "device pair",
			Summary:  "trade a pairing code for a device token, the credential --token expects",
			Endpoint: "POST /api/v1/devices/pair",
			Flags: []Flag{
				{Name: "code", Kind: KindString, Required: true,
					Description: "pairing code from `reflow device pairing-code`"},
				{Name: "name", Kind: KindString, Required: true,
					Description: "device label shown in 偏好设置 → 设备, so a human can revoke it"},
				{Name: "platform", Kind: KindString, Default: "cli", Description: "device platform"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				platform := v.String("platform")
				if platform == "" {
					platform = "cli"
				}
				raw, err := app.client.Call(ctx, "POST", "/api/v1/devices/pair", nil, map[string]any{
					"code": v.String("code"), "name": v.String("name"), "platform": platform,
				})
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
	}
}

func aiCommands() []Command {
	return []Command{
		{
			Path:     "ai profiles",
			Summary:  "list the configured AI profiles, whose id the AI commands take",
			Endpoint: "GET /api/v1/ai/profiles",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw, err := app.client.Get(ctx, "/api/v1/ai/profiles", nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "ai fill",
			Summary:  "extract bibliographic fields from one pasted reference, through the server's own model call",
			Endpoint: "POST /api/v1/ai/metadata-fill",
			Flags: []Flag{
				{Name: "raw", Kind: KindString, Required: true,
					Description: "the reference text, or `-` to read it from stdin"},
				{Name: "profile-id", Kind: KindString,
					Description: "AI profile to use; empty asks the server for its default"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw := v.String("raw")
				if raw == "-" {
					body, err := io.ReadAll(stdin)
					if err != nil {
						return &TransportError{Reason: "read stdin: " + err.Error()}
					}
					raw = strings.TrimSpace(string(body))
				}
				if raw == "" {
					return usage("--raw needs text, or `-` to read stdin")
				}
				// The caller quotes a whole reference per field name, so an empty profile
				// id is left out rather than sent as a value the server must ignore.
				body := map[string]any{"raw": raw}
				if profile := v.String("profile-id"); profile != "" {
					body["profile_id"] = profile
				}
				// A provider call is slower than the local CRUD commands, and the server
				// bounds it with its own timeout; the CLI's must not cut it short.
				filled, err := app.client.Call(ctx, "POST", "/api/v1/ai/metadata-fill", nil, body)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(filled))
			},
		},
	}
}
