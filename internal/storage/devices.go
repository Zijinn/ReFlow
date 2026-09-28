package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

var ErrInvalidPairingCode = errors.New("pairing code is invalid or expired")

func CreatePairingCode(ctx context.Context, db *sql.DB, lifetime time.Duration) (string, time.Time, error) {
	if lifetime <= 0 {
		lifetime = 10 * time.Minute
	}
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return "", time.Time{}, fmt.Errorf("generate pairing code: %w", err)
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	expiresAt := time.Now().UTC().Add(lifetime)
	digest := sha256.Sum256([]byte(code))
	if _, err := db.ExecContext(ctx, `
		DELETE FROM pairing_codes WHERE used_at IS NOT NULL OR expires_at <= ?`, formatTime(time.Now().UTC())); err != nil {
		return "", time.Time{}, fmt.Errorf("clean pairing codes: %w", err)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO pairing_codes (id, code_hash, expires_at) VALUES (?, ?, ?)`, uuid.NewString(), digest[:], formatTime(expiresAt))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("store pairing code: %w", err)
	}
	return code, expiresAt, nil
}

func PairDevice(ctx context.Context, db *sql.DB, code, name, platform string) (domain.Device, string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	platform = strings.ToLower(strings.TrimSpace(platform))
	if code == "" || name == "" || !validPlatform(platform) {
		return domain.Device{}, "", ErrInvalidPairingCode
	}
	digest := sha256.Sum256([]byte(code))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Device{}, "", err
	}
	defer tx.Rollback()
	var pairingID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM pairing_codes WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
		digest[:], formatTime(time.Now().UTC())).Scan(&pairingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Device{}, "", ErrInvalidPairingCode
		}
		return domain.Device{}, "", err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return domain.Device{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenDigest := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	device := domain.Device{ID: uuid.NewString(), Name: name, Platform: platform, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO devices (id, profile_id, name, platform, token_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, device.ID, domain.DefaultProfileID, name, platform, tokenDigest[:], formatTime(now)); err != nil {
		return domain.Device{}, "", fmt.Errorf("create device: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE pairing_codes SET used_at = ? WHERE id = ? AND used_at IS NULL", formatTime(now), pairingID); err != nil {
		return domain.Device{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return domain.Device{}, "", err
	}
	return device, token, nil
}

func AuthenticateDevice(ctx context.Context, db *sql.DB, token string) (domain.Device, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.Device{}, ErrNotFound
	}
	digest := sha256.Sum256([]byte(token))
	var device domain.Device
	var lastSeen, createdAt, revokedAt sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, name, platform, last_seen_at, created_at, revoked_at
		FROM devices WHERE token_hash = ? AND revoked_at IS NULL`, digest[:]).Scan(
		&device.ID, &device.Name, &device.Platform, &lastSeen, &createdAt, &revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, ErrNotFound
	}
	if err != nil {
		return domain.Device{}, err
	}
	device.LastSeenAt = timePointer(lastSeen)
	device.CreatedAt = parseTime(createdAt.String)
	device.RevokedAt = timePointer(revokedAt)
	// Throttle the bookkeeping write: one update per minute per device instead
	// of a write transaction per authenticated request.
	now := time.Now().UTC()
	if device.LastSeenAt == nil || now.Sub(*device.LastSeenAt) > time.Minute {
		_, _ = db.ExecContext(ctx, "UPDATE devices SET last_seen_at = ? WHERE id = ?", formatTime(now), device.ID)
		device.LastSeenAt = &now
	}
	return device, nil
}

func ListDevices(ctx context.Context, db *sql.DB, profileID string) ([]domain.Device, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, platform, last_seen_at, created_at, revoked_at
		FROM devices WHERE profile_id = ? ORDER BY revoked_at IS NOT NULL, created_at DESC`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Device, 0)
	for rows.Next() {
		var item domain.Device
		var lastSeen, createdAt, revokedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &lastSeen, &createdAt, &revokedAt); err != nil {
			return nil, err
		}
		item.LastSeenAt = timePointer(lastSeen)
		item.CreatedAt = parseTime(createdAt.String)
		item.RevokedAt = timePointer(revokedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func RevokeDevice(ctx context.Context, db *sql.DB, profileID, deviceID string) error {
	now := time.Now().UTC()
	result, err := db.ExecContext(ctx, `UPDATE devices SET revoked_at = ? WHERE id = ? AND profile_id = ? AND revoked_at IS NULL`, formatTime(now), deviceID, profileID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func CountActiveDevices(ctx context.Context, db *sql.DB) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE revoked_at IS NULL").Scan(&count)
	return count, err
}

func validPlatform(value string) bool {
	switch value {
	case "web", "ipad", "windows", "macos", "ios", "android":
		return true
	default:
		return false
	}
}
