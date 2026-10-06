package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
)

const (
	feedsPath         = "/api/v1/feeds"
	subscriptionsPath = "/api/v1/subscriptions"
	foldersPath       = "/api/v1/folders"
	opmlImportPath    = "/api/v1/imports/opml"
	opmlExportPath    = "/api/v1/exports/opml"
)

// subscriptionCommands covers the RSS side: the subscriptions an agent edits, the
// folders it files them into, and the OPML pair that moves a whole list in and out.
// Every endpoint here already served the app; what these commands add is the feed view
// (the feed record with its subscription record attached, so a filing you just made is
// readable), the field table check (a misspelled `--set` name refuses at exit 2 instead
// of answering 200 while writing nothing) and the --yes gate on the two deletes, because
// unsubscribing from a feed takes its articles with it.
func subscriptionCommands() []Command {
	return []Command{
		{
			Path:     "feed list",
			Summary:  "list subscriptions: each feed record with its folder, display title, view mode and unread count attached",
			Endpoint: "GET " + feedsPath + ", GET " + subscriptionsPath,
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw, err := app.client.Get(ctx, feedsPath, nil)
				if err != nil {
					return err
				}
				index, err := subscriptionIndex(ctx, app)
				if err != nil {
					return err
				}
				var page struct {
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(raw, &page); err != nil {
					return &TransportError{Reason: "decode " + feedsPath + ": " + err.Error()}
				}
				items := make([]any, 0, len(page.Items))
				for _, record := range page.Items {
					row, err := feedRow(record)
					if err != nil {
						return err
					}
					row["subscription"] = index[idOf(row)]
					items = append(items, row)
				}
				return app.Emit(map[string]any{"items": items})
			},
		},
		{
			Path:     "feed get",
			Summary:  "read one subscription: address, folder, display title, refresh policy, last fetch",
			Args:     "<feed-id>",
			Endpoint: "GET " + feedsPath + "/{feedID}, GET " + subscriptionsPath,
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "feed get")
				if err != nil {
					return err
				}
				raw, err := app.client.Get(ctx, feedsPath+"/"+url.PathEscape(id), nil)
				if err != nil {
					return err
				}
				view, err := feedView(ctx, app, raw)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(view))
			},
		},
		{
			Path:     "feed add",
			Summary:  "subscribe to one feed URL",
			Args:     "<feed-url>",
			Endpoint: "POST " + feedsPath + ", GET " + subscriptionsPath,
			Flags: []Flag{
				{Name: "folder", Kind: KindString, Description: "folder id to file the new subscription under"},
				{Name: "title", Kind: KindString, Description: "name to show instead of the feed's own title"},
			},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				target, err := requireOneArg(args, "feed add")
				if err != nil {
					return err
				}
				// Both extras stay out of the body when unset: the handler reads an empty
				// title_override as an override, which would blank the subscription's name.
				body := map[string]any{"url": target}
				if folder := v.String("folder"); folder != "" {
					body["folder_id"] = folder
				}
				if title := v.String("title"); title != "" {
					body["title_override"] = title
				}
				// The server fetches and parses the address before it answers, so this is the
				// one subscription command that can outlast the default --timeout. The fetch
				// may still land on the server after the CLI gives up, so a timeout here is
				// worth checking with `feed list` rather than retrying blind.
				raw, err := app.client.Call(ctx, "POST", feedsPath, nil, body)
				if err != nil {
					return err
				}
				view, err := feedView(ctx, app, raw)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(view))
			},
		},
		{
			Path:     "feed discover",
			Summary:  "list the feed URLs one page publishes, for subscribing by a site's homepage",
			Args:     "<page-url>",
			Endpoint: "POST " + feedsPath + "/discover",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				target, err := requireOneArg(args, "feed discover")
				if err != nil {
					return err
				}
				raw, err := app.client.Call(ctx, "POST", feedsPath+"/discover", nil,
					map[string]any{"url": target})
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "feed set",
			Summary:  "edit a subscription in place: folder, display title, view mode, refresh policy, order",
			Args:     "<feed-id>",
			Endpoint: "PATCH " + feedsPath + "/{feedID}, GET " + feedsPath + "/{feedID}",
			Flags: []Flag{{Name: "set", Kind: KindStrings,
				Description: "subscription field assignment, repeatable: --set 'folder_id=null' --set refresh_policy=never" +
					" | fields: " + patchHint(feedFields)}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "feed set")
				if err != nil {
					return err
				}
				patch, err := buildPatch(feedFields, "feed", v.Strings("set"))
				if err != nil {
					return err
				}
				if len(patch) == 0 {
					return usage("feed set needs at least one --set field=value")
				}
				// PATCH answers with the subscription row, whose `id` is the subscription's own.
				// Reading the feed back keeps every RSS command answering in feed ids.
				written, err := app.client.Call(ctx, "PATCH", feedsPath+"/"+url.PathEscape(id), nil, patch)
				if err != nil {
					return err
				}
				record, err := app.client.Get(ctx, feedsPath+"/"+url.PathEscape(id), nil)
				if err != nil {
					return err
				}
				row, err := feedRow(record)
				if err != nil {
					return err
				}
				row["subscription"] = json.RawMessage(written)
				return app.Emit(row)
			},
		},
		{
			Path:     "feed delete",
			Summary:  "unsubscribe; the feed's articles and their read states are deleted with it, gated behind --yes",
			Args:     "<feed-id>",
			Endpoint: "DELETE " + feedsPath + "/{feedID}",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "feed delete")
				if err != nil {
					return err
				}
				if err := app.Confirm("feed delete " + id); err != nil {
					return err
				}
				// entries.feed_id, entry_states and feed_tags all cascade from feeds, so the
				// articles a refresh pulled in leave with the subscription. Nothing in the API
				// keeps them, which is why this is the one RSS command that says so in its name.
				if _, err := app.client.Call(ctx, "DELETE", feedsPath+"/"+url.PathEscape(id), nil, nil); err != nil {
					return err
				}
				return app.Emit(map[string]any{"deleted": id})
			},
		},
		{
			Path:     "feed refresh",
			Summary:  "queue a fetch for one feed, then read the job it returns",
			Args:     "<feed-id>",
			Endpoint: "POST " + feedsPath + "/{feedID}/refresh",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "feed refresh")
				if err != nil {
					return err
				}
				raw, err := app.client.Call(ctx, "POST",
					feedsPath+"/"+url.PathEscape(id)+"/refresh", nil, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "folder list",
			Summary:  "list the folders subscriptions are filed into, with their ids",
			Endpoint: "GET " + foldersPath,
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				raw, err := app.client.Get(ctx, foldersPath, nil)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "folder add",
			Summary:  "create a folder, or answer with the one that already has this name under the same parent",
			Args:     "<name>",
			Endpoint: "POST " + foldersPath,
			Flags:    []Flag{{Name: "parent", Kind: KindString, Description: "parent folder id; empty makes a top-level folder"}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				name, err := requireOneArg(args, "folder add")
				if err != nil {
					return err
				}
				// The endpoint is an ensure, not an insert, so re-running this for the same
				// name returns the same id: an agent can make a folder part of a script.
				body := map[string]any{"name": name}
				if parent := v.String("parent"); parent != "" {
					body["parent_id"] = parent
				}
				raw, err := app.client.Call(ctx, "POST", foldersPath, nil, body)
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "folder set",
			Summary:  "rename a folder, move it under another parent, or reorder it",
			Args:     "<folder-id>",
			Endpoint: "PATCH " + foldersPath + "/{folderID}",
			Flags: []Flag{{Name: "set", Kind: KindStrings,
				Description: "folder field assignment, repeatable: --set name=文献 --set 'parent_id=null'" +
					" | fields: " + patchHint(folderFields)}},
			Handler: patchHandler(foldersPath, folderFields, "folder", "folder set"),
		},
		{
			Path:     "folder delete",
			Summary:  "delete a folder; the subscriptions inside it stay and become unfiled, gated behind --yes",
			Args:     "<folder-id>",
			Endpoint: "DELETE " + foldersPath + "/{folderID}",
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				id, err := requireOneArg(args, "folder delete")
				if err != nil {
					return err
				}
				if err := app.Confirm("folder delete " + id); err != nil {
					return err
				}
				if _, err := app.client.Call(ctx, "DELETE", foldersPath+"/"+url.PathEscape(id), nil, nil); err != nil {
					return err
				}
				return app.Emit(map[string]any{"deleted": id})
			},
		},
		{
			Path:     "opml import",
			Summary:  "subscribe from an OPML document in bulk; the answer is a job, follow it with `job get`",
			Endpoint: "POST " + opmlImportPath,
			Flags: []Flag{{Name: "file", Kind: KindString, Required: true,
				Description: "path to the .opml document, or `-` to read it from stdin"}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if len(args) > 0 {
					return usage("opml import takes no positional argument, got %q", args[0])
				}
				document, err := readDocument(v.String("file"))
				if err != nil {
					return err
				}
				// The document goes as its own body, not wrapped in JSON: the server parses the
				// bytes it is handed and the size limit for an import is its own, so a real
				// export from another reader stays within it.
				raw, err := app.client.SendDocument(ctx, "POST", opmlImportPath, nil, document, "text/xml; charset=utf-8")
				if err != nil {
					return err
				}
				return app.Emit(json.RawMessage(raw))
			},
		},
		{
			Path:     "opml export",
			Summary:  "write the subscription list as an OPML document",
			Endpoint: "GET " + opmlExportPath,
			Flags:    []Flag{{Name: "out", Kind: KindString, Description: "file to write; without it the document comes back inside the JSON envelope"}},
			Handler: func(ctx context.Context, app *App, v *Values, args []string) error {
				if len(args) > 0 {
					return usage("opml export takes no positional argument, got %q", args[0])
				}
				document, err := app.client.ReceiveDocument(ctx, opmlExportPath, nil)
				if err != nil {
					return err
				}
				if len(strings.TrimSpace(string(document))) == 0 {
					return &TransportError{Reason: "GET " + opmlExportPath + " returned an empty document"}
				}
				target := v.String("out")
				if target == "" {
					// Inline keeps the command usable without touching the disk; a caller that
					// wants a file passes --out rather than unwrapping this by hand.
					return app.Emit(map[string]any{"document": string(document)})
				}
				if err := os.WriteFile(target, document, 0o644); err != nil {
					return usage("write %s: %v", target, err)
				}
				return app.Emit(map[string]any{"path": target, "bytes": len(document)})
			},
		},
	}
}

// patchHandler is the `<group> set <id>` shape every PATCH endpoint in the CLI
// shares: take one id, turn --set assignments into a body the field table accepts,
// send it, print what the server wrote back.
func patchHandler(path string, table patchTable, noun, what string) func(context.Context, *App, *Values, []string) error {
	return func(ctx context.Context, app *App, v *Values, args []string) error {
		id, err := requireOneArg(args, what)
		if err != nil {
			return err
		}
		patch, err := buildPatch(table, noun, v.Strings("set"))
		if err != nil {
			return err
		}
		if len(patch) == 0 {
			return usage("%s needs at least one --set field=value", what)
		}
		raw, err := app.client.Call(ctx, "PATCH", path+"/"+url.PathEscape(id), nil, patch)
		if err != nil {
			return err
		}
		return app.Emit(json.RawMessage(raw))
	}
}

// readDocument takes a file path, or `-` for stdin the way `ai fill --raw -` does. A
// path that does not resolve is caller-fixable (exit 2); stdin failing is the pipe's
// fault, so it keeps the transport class.
func readDocument(path string) ([]byte, error) {
	if path == "-" {
		body, err := io.ReadAll(stdin)
		if err != nil {
			return nil, &TransportError{Reason: "read stdin: " + err.Error()}
		}
		return body, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, usage("read %s: %v", path, err)
	}
	return body, nil
}

// feedRow is one record from GET /api/v1/feeds, kept as a map so the CLI can attach the
// subscription without re-declaring every column the server answers with.
func feedRow(record json.RawMessage) (map[string]any, error) {
	var row map[string]any
	if err := json.Unmarshal(record, &row); err != nil {
		return nil, &TransportError{Reason: "decode " + feedsPath + ": " + err.Error()}
	}
	return row, nil
}

// idOf reads the feed id out of a feed record. It is the handle `feed get`, `feed set`,
// `feed delete` and `feed refresh` all expect, and not the subscription's own id.
func idOf(row map[string]any) string {
	id, _ := row["id"].(string)
	return id
}

// subscriptionIndex keys GET /api/v1/subscriptions by feed_id, the same join the app's
// sidebar makes. The server splits the two records: /feeds holds what the publisher said
// (title, description, content kind, next check) and /subscriptions holds what this
// library decided about it (folder, display title, view mode, unread count). Without the
// join the CLI could file a feed into a folder and never read that back.
func subscriptionIndex(ctx context.Context, app *App) (map[string]json.RawMessage, error) {
	raw, err := app.client.Get(ctx, subscriptionsPath, nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, &TransportError{Reason: "decode " + subscriptionsPath + ": " + err.Error()}
	}
	index := make(map[string]json.RawMessage, len(page.Items))
	for _, item := range page.Items {
		var row struct {
			FeedID string `json:"feed_id"`
		}
		if err := json.Unmarshal(item, &row); err != nil {
			return nil, &TransportError{Reason: "decode " + subscriptionsPath + ": " + err.Error()}
		}
		if row.FeedID != "" {
			index[row.FeedID] = item
		}
	}
	return index, nil
}

// feedView answers a single-feed command with the feed record and its subscription row
// under `subscription`, which is null when the feed has no subscription record.
func feedView(ctx context.Context, app *App, record json.RawMessage) (json.RawMessage, error) {
	row, err := feedRow(record)
	if err != nil {
		return nil, err
	}
	index, err := subscriptionIndex(ctx, app)
	if err != nil {
		return nil, err
	}
	row["subscription"] = index[idOf(row)]
	encoded, err := json.Marshal(row)
	if err != nil {
		return nil, &TransportError{Reason: "encode feed view: " + err.Error()}
	}
	return encoded, nil
}
