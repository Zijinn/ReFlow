package secretbox

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBoxPersistsKeyAndAuthenticatesContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.key")
	box, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Seal([]byte("secret"), []byte("account-1"))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := reloaded.Open(ciphertext, []byte("account-1"))
	if err != nil || !bytes.Equal(plaintext, []byte("secret")) {
		t.Fatalf("decrypt: %q, %v", plaintext, err)
	}
	if _, err := reloaded.Open(ciphertext, []byte("account-2")); err == nil {
		t.Fatal("expected associated data mismatch to fail")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected key permissions: %v, %v", info.Mode().Perm(), err)
	}
}

func TestLoadOrCreateRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.key")
	if err := os.WriteFile(target, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "master.key")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := LoadOrCreate(link); err == nil {
		t.Fatal("a symlink at the master key path must be rejected")
	}
}

func TestLoadOrCreateTightensExistingKeyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(path, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 key permissions, got %o", info.Mode().Perm())
	}
}
