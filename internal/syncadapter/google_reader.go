package syncadapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	googleReaderPageSize = 1000
	googleReaderBatch    = 200
)

type googleReaderAdapter struct{ baseClient }

func (a *googleReaderAdapter) Pull(ctx context.Context, cursor string) (Delta, error) {
	response, err := a.request(ctx, http.MethodGet, "reader/api/0/subscription/list", url.Values{"output": {"json"}}, nil)
	if err != nil {
		return Delta{}, err
	}
	var subscriptions struct {
		Subscriptions []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Categories []struct {
				Label string `json:"label"`
			} `json:"categories"`
		} `json:"subscriptions"`
	}
	if err := decodeJSONResponse(response, &subscriptions); err != nil {
		return Delta{}, err
	}
	delta := Delta{Subscriptions: make([]Subscription, 0, len(subscriptions.Subscriptions))}
	for _, item := range subscriptions.Subscriptions {
		folder := ""
		if len(item.Categories) > 0 {
			folder = item.Categories[0].Label
		}
		delta.Subscriptions = append(delta.Subscriptions, Subscription{
			RemoteID: item.ID, FeedURL: strings.TrimPrefix(item.ID, "feed/"), Title: item.Title, Folder: folder,
		})
	}
	unread, unreadCursor, err := a.pullIDs(ctx, "user/-/state/com.google/reading-list", "user/-/state/com.google/read", cursor, googleReaderPageSize)
	if err != nil {
		return Delta{}, err
	}
	// Reading-list + xt=read only returns items that are still unread. Pull a
	// recent slice of the read stream as well, otherwise an item marked read in
	// another client silently disappears instead of being applied locally.
	read, _, err := a.pullIDs(ctx, "user/-/state/com.google/read", "", "", 500)
	if err != nil {
		return Delta{}, err
	}
	starred, starredCursor, err := a.pullIDs(ctx, "user/-/state/com.google/starred", "", cursor, googleReaderPageSize)
	if err != nil {
		return Delta{}, err
	}
	states := make(map[string]ItemState)
	for _, id := range unread {
		states[id] = ItemState{RemoteID: id, Read: boolPointer(false)}
	}
	for _, id := range read {
		state := states[id]
		state.RemoteID = id
		state.Read = boolPointer(true)
		states[id] = state
	}
	for _, id := range starred {
		state := states[id]
		state.RemoteID = id
		state.Starred = boolPointer(true)
		states[id] = state
	}
	if err := a.enrichStates(ctx, states); err != nil {
		return Delta{}, err
	}
	for _, state := range states {
		delta.States = append(delta.States, state)
	}
	delta.Cursor = unreadCursor
	if starredCursor > delta.Cursor {
		delta.Cursor = starredCursor
	}
	return delta, nil
}

func (a *googleReaderAdapter) enrichStates(ctx context.Context, states map[string]ItemState) error {
	if len(states) == 0 {
		return nil
	}
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += googleReaderBatch {
		end := start + googleReaderBatch
		if end > len(ids) {
			end = len(ids)
		}
		values := url.Values{"output": {"json"}}
		for _, id := range ids[start:end] {
			values.Add("i", id)
		}
		response, err := a.form(ctx, "reader/api/0/stream/items/contents", nil, values)
		if err != nil {
			return err
		}
		var result struct {
			Items []struct {
				ID        string `json:"id"`
				Timestamp string `json:"timestampUsec"`
				Alternate []struct {
					Href string `json:"href"`
				} `json:"alternate"`
				Origin struct {
					StreamID string `json:"streamId"`
				} `json:"origin"`
			} `json:"items"`
		}
		if err := decodeJSONResponse(response, &result); err != nil {
			return err
		}
		for _, item := range result.Items {
			state, exists := states[item.ID]
			if !exists {
				continue
			}
			state.GUID = item.ID
			state.FeedRemoteID = item.Origin.StreamID
			state.RemoteUpdated = item.Timestamp
			if len(item.Alternate) > 0 {
				state.CanonicalURL = item.Alternate[0].Href
			}
			states[item.ID] = state
		}
	}
	return nil
}

func (a *googleReaderAdapter) pullIDs(ctx context.Context, stream, exclude, cursor string, pageSize int) ([]string, string, error) {
	ids := make([]string, 0)
	latest := cursor
	continuation := ""
	for page := 0; page < maxSyncPages; page++ {
		query := url.Values{"output": {"json"}, "s": {stream}, "n": {strconv.Itoa(pageSize)}}
		if exclude != "" {
			query.Set("xt", exclude)
		}
		if cursor != "" && continuation == "" {
			// nt is "newer than". Using ot here walks the cursor backwards and
			// permanently misses state changes created after the first sync.
			query.Set("nt", cursor)
		}
		if continuation != "" {
			query.Set("c", continuation)
		}
		response, err := a.request(ctx, http.MethodGet, "reader/api/0/stream/items/ids", query, nil)
		if err != nil {
			return nil, "", err
		}
		var result struct {
			ItemRefs []struct {
				ID        string `json:"id"`
				Timestamp string `json:"timestampUsec"`
			} `json:"itemRefs"`
			Continuation string `json:"continuation"`
		}
		if err := decodeJSONResponse(response, &result); err != nil {
			return nil, "", err
		}
		for _, item := range result.ItemRefs {
			ids = append(ids, item.ID)
			if item.Timestamp > latest {
				latest = item.Timestamp
			}
		}
		if result.Continuation == "" || result.Continuation == continuation {
			break
		}
		continuation = result.Continuation
	}
	return ids, latest, nil
}

func (a *googleReaderAdapter) Push(ctx context.Context, states []ItemState) error {
	for _, state := range states {
		values := url.Values{"i": {state.RemoteID}, "async": {"true"}}
		if state.Read != nil {
			key := "r"
			if *state.Read {
				key = "a"
			}
			values.Add(key, "user/-/state/com.google/read")
		}
		if state.Starred != nil {
			key := "r"
			if *state.Starred {
				key = "a"
			}
			values.Add(key, "user/-/state/com.google/starred")
		}
		response, err := a.form(ctx, "reader/api/0/edit-tag", nil, values)
		if err != nil {
			return fmt.Errorf("push Google Reader state %s: %w", state.RemoteID, err)
		}
		response.Body.Close()
	}
	return nil
}
