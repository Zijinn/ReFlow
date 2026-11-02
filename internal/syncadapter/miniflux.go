package syncadapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const minifluxPageSize = 1000

type minifluxAdapter struct{ baseClient }

func (a *minifluxAdapter) Pull(ctx context.Context, cursor string) (Delta, error) {
	feedsResponse, err := a.request(ctx, http.MethodGet, "v1/feeds", nil, nil)
	if err != nil {
		return Delta{}, err
	}
	var feeds []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		FeedURL  string `json:"feed_url"`
		Category struct {
			Title string `json:"title"`
		} `json:"category"`
	}
	if err := decodeJSONResponse(feedsResponse, &feeds); err != nil {
		return Delta{}, err
	}
	delta := Delta{Subscriptions: make([]Subscription, 0, len(feeds)), States: make([]ItemState, 0), Cursor: cursor}
	for _, item := range feeds {
		delta.Subscriptions = append(delta.Subscriptions, Subscription{RemoteID: strconv.FormatInt(item.ID, 10), Title: item.Title, FeedURL: item.FeedURL, Folder: item.Category.Title})
	}
	offset := 0
	for page := 0; page < maxSyncPages; page++ {
		query := url.Values{"limit": {strconv.Itoa(minifluxPageSize)}, "order": {"id"}, "direction": {"asc"}}
		if cursor != "" {
			query.Set("changed_after", cursor)
		}
		if offset > 0 {
			query.Set("offset", strconv.Itoa(offset))
		}
		entriesResponse, err := a.request(ctx, http.MethodGet, "v1/entries", query, nil)
		if err != nil {
			return Delta{}, err
		}
		var entries struct {
			Entries []struct {
				ID        int64  `json:"id"`
				FeedID    int64  `json:"feed_id"`
				Status    string `json:"status"`
				Starred   bool   `json:"starred"`
				URL       string `json:"url"`
				ChangedAt string `json:"changed_at"`
			} `json:"entries"`
		}
		if err := decodeJSONResponse(entriesResponse, &entries); err != nil {
			return Delta{}, err
		}
		for _, item := range entries.Entries {
			read := item.Status == "read" || item.Status == "removed"
			delta.States = append(delta.States, ItemState{
				RemoteID: strconv.FormatInt(item.ID, 10), FeedRemoteID: strconv.FormatInt(item.FeedID, 10),
				CanonicalURL: item.URL, Read: &read, Starred: &item.Starred, RemoteUpdated: item.ChangedAt,
			})
			if changedAt, parseErr := time.Parse(time.RFC3339, item.ChangedAt); parseErr == nil && changedAt.Unix() > parseInt64(delta.Cursor) {
				delta.Cursor = strconv.FormatInt(changedAt.Unix(), 10)
			}
		}
		if len(entries.Entries) < minifluxPageSize {
			break
		}
		offset += len(entries.Entries)
	}
	return delta, nil
}

func (a *minifluxAdapter) Push(ctx context.Context, states []ItemState) error {
	for _, state := range states {
		id, err := strconv.ParseInt(state.RemoteID, 10, 64)
		if err != nil {
			return err
		}
		if state.Read != nil {
			status := "unread"
			if *state.Read {
				status = "read"
			}
			response, err := a.request(ctx, http.MethodPut, "v1/entries", nil, map[string]any{"entry_ids": []int64{id}, "status": status})
			if err != nil {
				return fmt.Errorf("push Miniflux read state: %w", err)
			}
			response.Body.Close()
		}
		if state.Starred != nil {
			currentResponse, err := a.request(ctx, http.MethodGet, fmt.Sprintf("v1/entries/%d", id), nil, nil)
			if err != nil {
				return fmt.Errorf("read Miniflux bookmark: %w", err)
			}
			var current struct {
				Starred bool `json:"starred"`
			}
			if err := decodeJSONResponse(currentResponse, &current); err != nil {
				return err
			}
			if current.Starred == *state.Starred {
				continue
			}
			response, err := a.request(ctx, http.MethodPut, fmt.Sprintf("v1/entries/%d/bookmark", id), nil, nil)
			if err != nil {
				return fmt.Errorf("push Miniflux bookmark: %w", err)
			}
			response.Body.Close()
		}
	}
	return nil
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
