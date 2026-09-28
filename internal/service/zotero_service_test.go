package service

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type zoteroClientFunc func(*http.Request) (*http.Response, error)

func (function zoteroClientFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestParseArticleMetadataFindsCitationDOIAndAuthors(t *testing.T) {
	metadata := parseArticleMetadata([]byte(`<html><head>
		<meta name="citation_title" content="Metadata title">
		<meta name="citation_author" content="Ada Lovelace">
		<meta name="citation_author" content="Alan Turing">
		<meta name="dc.identifier" content="doi:10.1234/REFLOW.2026.1">
		<meta name="citation_journal_title" content="Systems Journal">
	</head></html>`))
	if metadata.DOI != "10.1234/REFLOW.2026.1" || metadata.Title != "Metadata title" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if len(metadata.Authors) != 2 || metadata.Authors[1] != "Alan Turing" {
		t.Fatalf("unexpected authors: %+v", metadata.Authors)
	}
}

func TestZoteroSaveWritesOnlyWhenExplicitlyCalled(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	author := "程康;俞乃畅;"
	created, err := storage.SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Test Journal", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: stringPointerForTest("one"), Title: "Manual Zotero export", Author: &author,
			PublishedAt: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC), ContentHash: "one",
		}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storage.ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: created.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list entries: %+v %v", page, err)
	}
	imports := 0
	service := NewZoteroService(db, nil)
	service.client = zoteroClientFunc(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch {
		case request.URL.Path == "/connector/getSelectedCollection":
			body = `{"editable":true,"libraryID":"1","libraryName":"My Library","id":"C1","name":"Research"}`
		case request.URL.Path == "/api/users/0/items/top":
			body = `[]`
		case request.URL.Path == "/connector/import":
			imports++
			payload, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(payload), "AU  - 程康") || !strings.Contains(string(payload), "AU  - 俞乃畅") {
				t.Fatalf("unexpected RIS: %s", payload)
			}
			body = `[{"key":"ABCD1234"}]`
		default:
			t.Fatalf("unexpected Zotero request: %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})

	if imports != 0 {
		t.Fatal("Zotero import happened before an explicit save")
	}
	result, err := service.Save(ctx, page.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if imports != 1 || !result.Saved || result.Export.ZoteroItemKey != "ABCD1234" {
		t.Fatalf("unexpected save result: imports=%d result=%+v", imports, result)
	}
}

func stringPointerForTest(value string) *string { return &value }
