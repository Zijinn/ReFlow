package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/Zijinn/ReFlow/internal/syncadapter"
)

func TestLibrarySyncCanMirrorWebDAVAndICloudTogether(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("iCloud Drive is available only on macOS and Windows")
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Mirrored feed", Format: "rss", Entries: []domain.ParsedEntry{{
			Title: "Mirrored article", PublishedAt: time.Now().UTC(), ContentHash: "mirrored",
			SanitizedHTML: "<p>Mirrored</p>", PlainText: "Mirrored",
		}},
	}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var webDAVBody []byte
	webDAV := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if len(webDAVBody) == 0 {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(webDAVBody)
		case http.MethodPut:
			webDAVBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	defer webDAV.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	iCloudFile := filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs", "ReFlow", "reflow-library.json")
	syncService := newSyncService(db, NewFeedService(db, nil), box, func(bool) *http.Client { return webDAV.Client() })
	webDAVAccount, err := syncService.CreateAccount(ctx, SyncAccountInput{
		Provider: "webdav", Name: "WebDAV mirror", Endpoint: webDAV.URL + "/reflow-library.json",
		Credentials: syncadapter.Credentials{Username: "reflow", Password: "secret"}, AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	iCloudAccount, err := syncService.CreateAccount(ctx, SyncAccountInput{Provider: "icloud", Name: "iCloud mirror", Endpoint: iCloudFile})
	if err != nil {
		t.Fatal(err)
	}
	webDAVResult, err := syncService.Run(ctx, webDAVAccount.ID, nil)
	if err != nil || webDAVResult.Action != "push" {
		t.Fatalf("WebDAV initial mirror failed: %+v %v", webDAVResult, err)
	}
	iCloudResult, err := syncService.Run(ctx, iCloudAccount.ID, nil)
	if err != nil || iCloudResult.Action != "push" {
		t.Fatalf("iCloud initial mirror failed: %+v %v", iCloudResult, err)
	}
	mu.Lock()
	webDAVWritten := len(webDAVBody) > 0
	mu.Unlock()
	if !webDAVWritten {
		t.Fatal("WebDAV mirror did not receive a snapshot")
	}
	if info, err := os.Stat(iCloudFile); err != nil || info.Size() == 0 {
		t.Fatalf("iCloud mirror was not written: %v", err)
	}
	accounts, err := syncService.ListAccounts(ctx)
	if err != nil || len(accounts) != 2 || !accounts[0].Enabled || !accounts[1].Enabled {
		t.Fatalf("cloud targets were not enabled together: %+v %v", accounts, err)
	}
}

func TestICloudRootsRejectUnsupportedPlatforms(t *testing.T) {
	if _, err := iCloudRootsForPlatform("linux", t.TempDir()); err == nil {
		t.Fatal("expected Linux iCloud Drive synchronization to be rejected")
	}

	home := t.TempDir()
	darwinRoots, err := iCloudRootsForPlatform("darwin", home)
	if err != nil || len(darwinRoots) != 1 || darwinRoots[0] != filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs") {
		t.Fatalf("unexpected macOS roots: %v %v", darwinRoots, err)
	}
	windowsRoots, err := iCloudRootsForPlatform("windows", home)
	if err != nil || len(windowsRoots) != 1 || windowsRoots[0] != filepath.Join(home, "iCloudDrive") {
		t.Fatalf("unexpected Windows roots: %v %v", windowsRoots, err)
	}
}

func TestLibrarySyncRequiresConflictChoice(t *testing.T) {
	local := storage.BackupDocument{Format: storage.LibrarySnapshotFormat, Version: 1, Tables: []storage.BackupTable{{Name: "feeds", Rows: [][]storage.BackupValue{{{Kind: "text", Text: "local"}}}}}}
	action, err := chooseLibrarySyncAction(local, "local-hash", true, "remote-hash", librarySyncCursor{})
	if err == nil || action != "" || syncadapter.ErrorCode(err) != "conflict" {
		t.Fatalf("expected an explicit first-sync conflict, got action=%q err=%v", action, err)
	}
	action, err = chooseLibrarySyncAction(local, "local-new", true, "remote-new", librarySyncCursor{LocalFingerprint: "local-old", RemoteFingerprint: "remote-old"})
	if err == nil || action != "" || syncadapter.ErrorCode(err) != "conflict" {
		t.Fatalf("expected a two-sided conflict, got action=%q err=%v", action, err)
	}
}

func TestWebDAVDirectoryEndpointGetsSnapshotFilename(t *testing.T) {
	got, err := normalizeLibrarySyncEndpoint("webdav", "https://dav.example.test/ReFlow/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://dav.example.test/ReFlow/reflow-library.json" {
		t.Fatalf("unexpected normalized endpoint: %q", got)
	}
	nutstore, err := normalizeLibrarySyncEndpoint("webdav", "https://dav.jianguoyun.com/dav/")
	if err != nil {
		t.Fatal(err)
	}
	if nutstore != "https://dav.jianguoyun.com/dav/ReFlow/reflow-library.json" {
		t.Fatalf("unexpected Nutstore endpoint: %q", nutstore)
	}
	legacy, err := normalizeLibrarySyncEndpoint("webdav", "https://dav.jianguoyun.com/dav/reflow-library.json")
	if err != nil {
		t.Fatal(err)
	}
	if legacy != nutstore {
		t.Fatalf("legacy Nutstore endpoint was not migrated: %q", legacy)
	}
}

func TestWebDAVConnectionCreatesCollectionAndChecksWriteAccess(t *testing.T) {
	collectionCreated := false
	testFileWritten := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/ReFlow/":
			if collectionCreated {
				w.WriteHeader(http.StatusMultiStatus)
			} else {
				http.NotFound(w, r)
			}
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/":
			w.WriteHeader(http.StatusMultiStatus)
		case r.Method == "MKCOL" && r.URL.Path == "/dav/ReFlow/":
			collectionCreated = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/dav/ReFlow/reflow-connection-test-"):
			testFileWritten = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/dav/ReFlow/reflow-connection-test-"):
			if !testFileWritten {
				http.NotFound(w, r)
				return
			}
			testFileWritten = false
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected WebDAV request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()

	service := &SyncService{clientFactory: func(bool) *http.Client { return upstream.Client() }}
	err := service.testWebDAVConnection(
		context.Background(),
		upstream.URL+"/dav/ReFlow/reflow-library.json",
		syncadapter.Credentials{Username: "user", Password: "app-password"},
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !collectionCreated || testFileWritten {
		t.Fatalf("connection test did not prepare and clean up the collection: created=%v test_file=%v", collectionCreated, testFileWritten)
	}
}

func TestWebDAVSnapshotWriteCreatesMissingCollectionAndRetries(t *testing.T) {
	collectionCreated := false
	writes := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/dav/ReFlow/reflow-library.json":
			writes++
			if !collectionCreated {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/ReFlow/":
			http.NotFound(w, r)
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/":
			w.WriteHeader(http.StatusMultiStatus)
		case r.Method == "MKCOL" && r.URL.Path == "/dav/ReFlow/":
			collectionCreated = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected WebDAV request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()

	service := &SyncService{clientFactory: func(bool) *http.Client { return upstream.Client() }}
	record := storage.SyncAccountRecord{Account: domain.SyncAccount{
		Provider:            "webdav",
		Endpoint:            upstream.URL + "/dav/ReFlow/reflow-library.json",
		AllowPrivateNetwork: true,
	}}
	document := storage.BackupDocument{Format: storage.LibrarySnapshotFormat, Version: 1}
	if err := service.writeRemoteSnapshot(context.Background(), record, syncadapter.Credentials{}, document); err != nil {
		t.Fatal(err)
	}
	if !collectionCreated || writes != 2 {
		t.Fatalf("snapshot write did not create and retry: created=%v writes=%d", collectionCreated, writes)
	}
}
