package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

const entriesPath = "/api/v1/entries"

// contentKinds are the timeline's source categories. A literature feed is one the
// server classified from its site URL and its DOI density.
var contentKinds = []string{"general", "literature", "video", "social"}

var entryStates = []string{"all", "unread", "starred", "read_later"}

func entryCommands() []Command {
	return []Command{
		{
			Path:     "entry list",
			Summary:  "read one page of the timeline",
			Endpoint: "GET " + entriesPath,
			Flags: []Flag{
				{Name: "kind", Kind: KindChoice, Choices: contentKinds,
					Description: "only entries of feeds in this category; literature is the one that carries papers"},
				{Name: "state", Kind: KindChoice, Choices: entryStates, Description: "read/unread/starred filter"},
				{Name: "query", Kind: KindString, Description: "full-text search"},
				{Name: "feed", Kind: KindString, Description: "limit to one feed id"},
				{Name: "folder", Kind: KindString, Description: "limit to one folder id"},
				{Name: "tag-id", Kind: KindString, Description: "limit to entries carrying this reader label"},
				{Name: "since", Kind: KindString,
					Description: "time boundary: RFC3339, or a YYYY-MM-DD date read as midnight UTC"},
				{Name: "days", Kind: KindInt, Description: "shorthand for --since, N days back from now"},
				{Name: "limit", Kind: KindInt, Default: "30", Description: "page size, 1-100"},
				{Name: "cursor", Kind: KindString, Description: "next_cursor from the previous page"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				query, err := entryQuery(v, app.now)
				if err != nil {
					return err
				}
				raw, err := app.client.Get(ctx, entriesPath, query)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "entry get",
			Summary:  "read one entry, including its DOI when the feed carries one",
			Args:     "<entry-id>",
			Endpoint: "GET " + entriesPath + "/{entryID}",
			Flags:    []Flag{{Name: "ai-language", Kind: KindString, Description: "fetch the translated AI summary in this language"}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "entry get")
				if err != nil {
					return err
				}
				query := url.Values{}
				if language := v.String("ai-language"); language != "" {
					query.Set("ai_language", language)
				}
				raw, err := app.client.Get(ctx, entriesPath+"/"+url.PathEscape(id), query)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
	}
}

// entryQuery builds the timeline query string from the shared flag names.
func entryQuery(v *Values, now func() time.Time) (url.Values, error) {
	query := url.Values{}
	if kind := v.String("kind"); kind != "" {
		query.Set("content_kind", kind)
	}
	if state := v.String("state"); state != "" {
		query.Set("state", state)
	}
	if search := v.String("query"); search != "" {
		query.Set("query", search)
	}
	if feed := v.String("feed"); feed != "" {
		query.Set("feed_id", feed)
	}
	if folder := v.String("folder"); folder != "" {
		query.Set("folder_id", folder)
	}
	if tag := v.String("tag-id"); tag != "" {
		query.Set("tag_id", tag)
	}
	if cursor := v.String("cursor"); cursor != "" {
		query.Set("cursor", cursor)
	}
	since, err := sinceValue(v, now)
	if err != nil {
		return nil, err
	}
	if since != "" {
		query.Set("since", since)
	}
	if v.Changed("limit") {
		limit := v.Int("limit")
		if limit < 1 || limit > 100 {
			return nil, usage("--limit must be between 1 and 100, got %d", limit)
		}
		query.Set("limit", strconv.Itoa(limit))
	}
	return query, nil
}

// sinceValue resolves --since / --days into the RFC3339 boundary the API expects.
// The server only reads RFC3339, so a date-only form is normalised here rather
// than answered with a 400 the caller has to decode.
func sinceValue(v *Values, now func() time.Time) (string, error) {
	raw := v.String("since")
	days := v.Int("days")
	if raw != "" && v.Changed("days") {
		return "", usage("pass either --since or --days, not both")
	}
	if v.Changed("days") {
		if days < 0 {
			return "", usage("--days must not be negative")
		}
		return now().UTC().AddDate(0, 0, -days).Format(time.RFC3339), nil
	}
	if raw == "" {
		return "", nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC().Format(time.RFC3339), nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed.UTC().Format(time.RFC3339), nil
	}
	return "", usage(`--since must be RFC3339 or YYYY-MM-DD, got %q`, raw)
}
