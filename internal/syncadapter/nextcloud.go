package syncadapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type nextcloudAdapter struct{ baseClient }

func (a *nextcloudAdapter) Pull(ctx context.Context, cursor string) (Delta, error) {
	feedsResponse, err := a.request(ctx, http.MethodGet, "index.php/apps/news/api/v1-3/feeds", nil, nil)
	if err != nil {
		return Delta{}, err
	}
	var feeds struct {
		Feeds []struct {
			ID       int64  `json:"id"`
			URL      string `json:"url"`
			Title    string `json:"title"`
			FolderID *int64 `json:"folderId"`
		} `json:"feeds"`
	}
	if err := decodeJSONResponse(feedsResponse, &feeds); err != nil {
		return Delta{}, err
	}
	route := "index.php/apps/news/api/v1-3/items"
	query := url.Values{"type": {"3"}, "getRead": {"true"}, "batchSize": {"1000"}}
	if cursor != "" {
		// offset is a page offset, not an item ID. Use the endpoint that reports
		// every item changed since the stored lastModified cursor.
		route = "index.php/apps/news/api/v1-3/items/updated"
		query.Set("lastModified", cursor)
	}
	itemsResponse, err := a.request(ctx, http.MethodGet, route, query, nil)
	if err != nil {
		return Delta{}, err
	}
	var items struct {
		Items []struct {
			ID           int64  `json:"id"`
			FeedID       int64  `json:"feedId"`
			GUID         string `json:"guid"`
			URL          string `json:"url"`
			Unread       bool   `json:"unread"`
			Starred      bool   `json:"starred"`
			LastModified int64  `json:"lastModified"`
		} `json:"items"`
	}
	if err := decodeJSONResponse(itemsResponse, &items); err != nil {
		return Delta{}, err
	}
	delta := Delta{Cursor: cursor, Subscriptions: make([]Subscription, 0, len(feeds.Feeds)), States: make([]ItemState, 0, len(items.Items))}
	for _, item := range feeds.Feeds {
		folder := ""
		if item.FolderID != nil {
			folder = strconv.FormatInt(*item.FolderID, 10)
		}
		delta.Subscriptions = append(delta.Subscriptions, Subscription{RemoteID: strconv.FormatInt(item.ID, 10), Title: item.Title, FeedURL: item.URL, Folder: folder})
	}
	for _, item := range items.Items {
		read := !item.Unread
		delta.States = append(delta.States, ItemState{
			RemoteID: strconv.FormatInt(item.ID, 10), FeedRemoteID: strconv.FormatInt(item.FeedID, 10),
			GUID: item.GUID, CanonicalURL: item.URL, Read: &read, Starred: &item.Starred,
			RemoteUpdated: strconv.FormatInt(item.LastModified, 10),
		})
		if item.LastModified > parseInt64(delta.Cursor) {
			delta.Cursor = strconv.FormatInt(item.LastModified, 10)
		}
	}
	return delta, nil
}

func (a *nextcloudAdapter) Push(ctx context.Context, states []ItemState) error {
	for _, state := range states {
		if _, err := strconv.ParseInt(state.RemoteID, 10, 64); err != nil {
			return err
		}
		if state.Read != nil {
			action := "unread"
			if *state.Read {
				action = "read"
			}
			response, err := a.request(ctx, http.MethodPut, fmt.Sprintf("index.php/apps/news/api/v1-3/items/%s/%s", state.RemoteID, action), nil, nil)
			if err != nil {
				return err
			}
			response.Body.Close()
		}
		if state.Starred != nil {
			action := "unstar"
			if *state.Starred {
				action = "star"
			}
			response, err := a.request(ctx, http.MethodPut, fmt.Sprintf("index.php/apps/news/api/v1-3/items/%s/%s", state.RemoteID, action), nil, nil)
			if err != nil {
				return err
			}
			response.Body.Close()
		}
	}
	return nil
}
