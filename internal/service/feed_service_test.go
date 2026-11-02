package service

import (
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestLimitParsedEntriesKeepsNewestItems(t *testing.T) {
	entries := make([]domain.ParsedEntry, maxEntriesPerRefresh+2)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for index := range entries {
		entries[index] = domain.ParsedEntry{
			Title:       "entry",
			PublishedAt: base.Add(time.Duration(index) * time.Minute),
		}
	}
	newest := entries[len(entries)-1].PublishedAt
	oldestKept := entries[len(entries)-maxEntriesPerRefresh].PublishedAt
	parsed := limitParsedEntries(domain.ParsedFeed{Entries: entries})
	if len(parsed.Entries) != maxEntriesPerRefresh {
		t.Fatalf("kept %d entries, want %d", len(parsed.Entries), maxEntriesPerRefresh)
	}
	if !parsed.Entries[0].PublishedAt.Equal(newest) {
		t.Fatalf("newest entry was not kept first: %v", parsed.Entries[0].PublishedAt)
	}
	if parsed.Entries[len(parsed.Entries)-1].PublishedAt.Before(oldestKept) {
		t.Fatal("limit kept older entries instead of the newest window")
	}
}
