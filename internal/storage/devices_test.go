package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPairAuthenticateAndRevokeDevice(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	code, _, err := CreatePairingCode(ctx, db, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	device, token, err := PairDevice(ctx, db, code, "My iPad", "ipad")
	if err != nil || token == "" {
		t.Fatalf("pair device: %+v token=%q err=%v", device, token, err)
	}
	if _, _, err := PairDevice(ctx, db, code, "Second iPad", "ipad"); !errors.Is(err, ErrInvalidPairingCode) {
		t.Fatalf("pairing code should be one-time, got %v", err)
	}
	authenticated, err := AuthenticateDevice(ctx, db, token)
	if err != nil || authenticated.ID != device.ID || authenticated.LastSeenAt == nil {
		t.Fatalf("authenticate: %+v, %v", authenticated, err)
	}
	if err := RevokeDevice(ctx, db, "00000000-0000-4000-8000-000000000001", device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateDevice(ctx, db, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token should fail, got %v", err)
	}
}
