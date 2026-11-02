package event

import (
	"testing"
	"time"
)

func TestSubscribeSinceReplaysOnlyMissedEvents(t *testing.T) {
	hub := NewHub()
	hub.Publish("first", map[string]string{"id": "1"})
	firstID := hub.nextID.Load()
	hub.Publish("second", map[string]string{"id": "2"})

	channel, unsubscribe := hub.SubscribeSince(firstID)
	defer unsubscribe()
	select {
	case event := <-channel:
		if event.Type != "second" || event.ID != firstID+1 {
			t.Fatalf("unexpected replayed event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replayed event")
	}

	fresh, unsubscribeFresh := hub.Subscribe()
	defer unsubscribeFresh()
	select {
	case event := <-fresh:
		t.Fatalf("fresh subscriber received retained history: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHubPublishesToSubscribers(t *testing.T) {
	hub := NewHub()
	channel, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	hub.Publish("feed.updated", map[string]string{"id": "feed-1"})
	select {
	case event := <-channel:
		if event.Type != "feed.updated" || event.ID == 0 {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}
