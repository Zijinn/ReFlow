package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const keySize = 32

type Box struct {
	aead cipher.AEAD
}

func LoadOrCreate(path string) (*Box, error) {
	var key []byte
	info, lstatErr := os.Lstat(path)
	switch {
	case lstatErr == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("secret key path must be a regular file")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open secret key: %w", err)
		}
		openedInfo, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("inspect opened secret key: %w", err)
		}
		// Reject a path that was swapped between Lstat and Open (including a
		// regular file or symlink pointing elsewhere). Keep using the opened fd,
		// and chmod the fd rather than the path, to close the TOCTOU window.
		if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			file.Close()
			return nil, errors.New("secret key path changed while opening")
		}
		if err := file.Chmod(0o600); err != nil {
			file.Close()
			return nil, fmt.Errorf("secure secret key permissions: %w", err)
		}
		key, err = io.ReadAll(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("read secret key: %w", err)
		}
	case errors.Is(lstatErr, os.ErrNotExist):
		key = make([]byte, keySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate secret key: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create secret key directory: %w", err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return LoadOrCreate(path)
			}
			return nil, fmt.Errorf("create secret key: %w", err)
		}
		if _, err := file.Write(key); err != nil {
			file.Close()
			return nil, fmt.Errorf("write secret key: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close secret key: %w", err)
		}
	default:
		return nil, fmt.Errorf("inspect secret key: %w", lstatErr)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("secret key must be %d bytes", keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Seal(plaintext, associatedData []byte) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, errors.New("secret box is not configured")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, associatedData), nil
}

func (b *Box) Open(ciphertext, associatedData []byte) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, errors.New("secret box is not configured")
	}
	if len(ciphertext) < b.aead.NonceSize() {
		return nil, errors.New("encrypted value is truncated")
	}
	nonce := ciphertext[:b.aead.NonceSize()]
	return b.aead.Open(nil, nonce, ciphertext[b.aead.NonceSize():], associatedData)
}
