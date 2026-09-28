package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

const syncAccountColumns = `
	id, provider, name, endpoint, enabled, allow_private_network,
	sync_interval_minutes, last_sync_at, next_sync_at, last_attempt_at,
	last_error_code, last_error_message, created_at, updated_at`

type SyncAccountRecord struct {
	Account              domain.SyncAccount
	ProfileID            string
	EncryptedCredentials []byte
	Cursor               string
}

type CreateSyncAccountParams struct {
	ID                   string
	ProfileID            string
	Provider             string
	Name                 string
	Endpoint             string
	EncryptedCredentials []byte
	Enabled              bool
	AllowPrivateNetwork  bool
	SyncIntervalMinutes  int
}

type SyncAccountPatch struct {
	Name                    *string
	Endpoint                *string
	EncryptedCredentials    []byte
	SetEncryptedCredentials bool
	Enabled                 *bool
	AllowPrivateNetwork     *bool
	SyncIntervalMinutes     *int
}

type SyncStateChange struct {
	LocalEntryID string
	RemoteID     string
	Read         bool
	Starred      bool
	UpdatedAt    time.Time
}

func CreateSyncAccount(ctx context.Context, db *sql.DB, params CreateSyncAccountParams) (domain.SyncAccount, error) {
	if params.ID == "" {
		params.ID = uuid.NewString()
	}
	if params.ProfileID == "" {
		params.ProfileID = domain.DefaultProfileID
	}
	if params.SyncIntervalMinutes == 0 {
		params.SyncIntervalMinutes = 30
	}
	now := time.Now().UTC()
	nextSync := now.Add(time.Duration(params.SyncIntervalMinutes) * time.Minute)
	_, err := db.ExecContext(ctx, `
		INSERT INTO sync_accounts (
			id, profile_id, provider, name, endpoint, encrypted_credentials,
			enabled, allow_private_network, sync_interval_minutes, next_sync_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		params.ID, params.ProfileID, params.Provider, params.Name, params.Endpoint,
		params.EncryptedCredentials, boolInt(params.Enabled), boolInt(params.AllowPrivateNetwork),
		params.SyncIntervalMinutes, formatTime(nextSync), formatTime(now), formatTime(now),
	)
	if err != nil {
		return domain.SyncAccount{}, fmt.Errorf("create sync account: %w", err)
	}
	record, err := GetSyncAccountRecord(ctx, db, params.ProfileID, params.ID)
	return record.Account, err
}

func ListSyncAccounts(ctx context.Context, db *sql.DB, profileID string) ([]domain.SyncAccount, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+syncAccountColumns+`
		FROM sync_accounts WHERE profile_id = ? ORDER BY name COLLATE NOCASE, id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list sync accounts: %w", err)
	}
	defer rows.Close()
	items := make([]domain.SyncAccount, 0)
	for rows.Next() {
		account, scanErr := scanSyncAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, account)
	}
	return items, rows.Err()
}

func GetSyncAccountRecord(ctx context.Context, db *sql.DB, profileID, accountID string) (SyncAccountRecord, error) {
	row := db.QueryRowContext(ctx, `SELECT `+syncAccountColumns+`, profile_id,
		encrypted_credentials, cursor_json FROM sync_accounts WHERE profile_id = ? AND id = ?`, profileID, accountID)
	var record SyncAccountRecord
	var enabled, allowPrivate int
	var lastSync, nextSync, lastAttempt, errorCode, errorMessage sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(
		&record.Account.ID, &record.Account.Provider, &record.Account.Name, &record.Account.Endpoint,
		&enabled, &allowPrivate, &record.Account.SyncIntervalMinutes, &lastSync, &nextSync,
		&lastAttempt, &errorCode, &errorMessage, &createdAt, &updatedAt,
		&record.ProfileID, &record.EncryptedCredentials, &record.Cursor,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SyncAccountRecord{}, ErrNotFound
		}
		return SyncAccountRecord{}, fmt.Errorf("scan sync account secrets: %w", err)
	}
	record.Account.Enabled = enabled == 1
	record.Account.AllowPrivateNetwork = allowPrivate == 1
	record.Account.LastSyncAt = timePointer(lastSync)
	record.Account.NextSyncAt = timePointer(nextSync)
	record.Account.LastAttemptAt = timePointer(lastAttempt)
	record.Account.LastErrorCode = stringPointer(errorCode)
	record.Account.LastErrorMessage = stringPointer(errorMessage)
	record.Account.CreatedAt = parseTime(createdAt)
	record.Account.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

func UpdateSyncAccount(ctx context.Context, db *sql.DB, profileID, accountID string, patch SyncAccountPatch) (domain.SyncAccount, error) {
	record, err := GetSyncAccountRecord(ctx, db, profileID, accountID)
	if err != nil {
		return domain.SyncAccount{}, err
	}
	name := record.Account.Name
	if patch.Name != nil {
		name = *patch.Name
	}
	endpoint := record.Account.Endpoint
	if patch.Endpoint != nil {
		endpoint = *patch.Endpoint
	}
	credentials := record.EncryptedCredentials
	if patch.SetEncryptedCredentials {
		credentials = patch.EncryptedCredentials
	}
	enabled := record.Account.Enabled
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	allowPrivate := record.Account.AllowPrivateNetwork
	if patch.AllowPrivateNetwork != nil {
		allowPrivate = *patch.AllowPrivateNetwork
	}
	interval := record.Account.SyncIntervalMinutes
	if patch.SyncIntervalMinutes != nil {
		interval = *patch.SyncIntervalMinutes
	}
	now := time.Now().UTC()
	nextSync := now.Add(time.Duration(interval) * time.Minute)
	result, err := db.ExecContext(ctx, `
		UPDATE sync_accounts SET name = ?, endpoint = ?, encrypted_credentials = ?,
			enabled = ?, allow_private_network = ?, sync_interval_minutes = ?,
			next_sync_at = ?, updated_at = ?
		WHERE profile_id = ? AND id = ?`,
		name, endpoint, credentials, boolInt(enabled), boolInt(allowPrivate), interval,
		formatTime(nextSync), formatTime(now), profileID, accountID)
	if err != nil {
		return domain.SyncAccount{}, fmt.Errorf("update sync account: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.SyncAccount{}, ErrNotFound
	}
	updated, err := GetSyncAccountRecord(ctx, db, profileID, accountID)
	return updated.Account, err
}

func DeleteSyncAccount(ctx context.Context, db *sql.DB, profileID, accountID string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM sync_accounts WHERE profile_id = ? AND id = ?", profileID, accountID)
	if err != nil {
		return fmt.Errorf("delete sync account: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func ListDueSyncAccounts(ctx context.Context, db *sql.DB, limit int) ([]domain.SyncAccount, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `SELECT `+syncAccountColumns+`
		FROM sync_accounts
		WHERE enabled = 1 AND (next_sync_at IS NULL OR next_sync_at <= ?)
		ORDER BY COALESCE(next_sync_at, created_at), id LIMIT ?`, formatTime(time.Now().UTC()), limit)
	if err != nil {
		return nil, fmt.Errorf("list due sync accounts: %w", err)
	}
	defer rows.Close()
	items := make([]domain.SyncAccount, 0)
	for rows.Next() {
		account, scanErr := scanSyncAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, account)
	}
	return items, rows.Err()
}

func MarkSyncStarted(ctx context.Context, db *sql.DB, accountID string, startedAt time.Time) error {
	result, err := db.ExecContext(ctx, `UPDATE sync_accounts SET last_attempt_at = ?, updated_at = ? WHERE id = ?`,
		formatTime(startedAt), formatTime(startedAt), accountID)
	if err != nil {
		return fmt.Errorf("mark sync started: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func CompleteSync(ctx context.Context, db *sql.DB, accountID, cursor string, completedAt time.Time) error {
	// next_sync_at must stay RFC3339: datetime() emits a space-separated value
	// that sorts before every RFC3339 string on the same date, which makes the
	// account immediately due again after every successful sync.
	result, err := db.ExecContext(ctx, `
		UPDATE sync_accounts SET cursor_json = ?, last_sync_at = ?,
			next_sync_at = strftime('%Y-%m-%dT%H:%M:%fZ', ?, '+' || sync_interval_minutes || ' minutes'),
			last_error_code = NULL, last_error_message = NULL, updated_at = ?
		WHERE id = ?`, cursor, formatTime(completedAt), formatTime(completedAt), formatTime(completedAt), accountID)
	if err != nil {
		return fmt.Errorf("complete sync: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func FailSync(ctx context.Context, db *sql.DB, accountID, code, message string, retryAt time.Time) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	result, err := db.ExecContext(ctx, `
		UPDATE sync_accounts SET last_error_code = ?, last_error_message = ?,
			next_sync_at = ?, updated_at = ? WHERE id = ?`,
		code, message, formatTime(retryAt), formatTime(time.Now().UTC()), accountID)
	if err != nil {
		return fmt.Errorf("record sync failure: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func UpsertSyncMapping(ctx context.Context, db *sql.DB, accountID, localKind, localID, remoteID string) error {
	if localKind != "feed" && localKind != "entry" {
		return errors.New("unsupported sync mapping kind")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO sync_mappings (account_id, local_kind, local_id, remote_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, local_kind, local_id) DO UPDATE SET
			remote_id = excluded.remote_id, updated_at = excluded.updated_at`,
		accountID, localKind, localID, remoteID)
	if err != nil {
		return fmt.Errorf("upsert sync mapping: %w", err)
	}
	return nil
}

func FindSyncMappingByRemote(ctx context.Context, db *sql.DB, accountID, localKind, remoteID string) (string, error) {
	var localID string
	err := db.QueryRowContext(ctx, `SELECT local_id FROM sync_mappings
		WHERE account_id = ? AND local_kind = ? AND remote_id = ?`, accountID, localKind, remoteID).Scan(&localID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return localID, err
}

func FindFeedByCanonicalURL(ctx context.Context, db *sql.DB, profileID, canonicalURL string) (domain.Feed, error) {
	feed, err := scanFeed(db.QueryRowContext(ctx, `
		SELECT `+qualifiedFeedColumns("f")+` FROM feeds f
		JOIN subscriptions s ON s.feed_id = f.id
		WHERE s.profile_id = ? AND f.canonical_url = ?`, profileID, canonicalURL))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Feed{}, ErrNotFound
	}
	return feed, err
}

func FindEntryForSync(ctx context.Context, db *sql.DB, accountID, remoteID, feedRemoteID, guid, canonicalURL string) (string, error) {
	if localID, err := FindSyncMappingByRemote(ctx, db, accountID, "entry", remoteID); err == nil {
		return localID, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if canonicalURL != "" {
		var entryID string
		err := db.QueryRowContext(ctx, `SELECT e.id FROM entries e
			JOIN subscriptions s ON s.feed_id = e.feed_id
			JOIN sync_accounts a ON a.profile_id = s.profile_id
			WHERE a.id = ? AND e.canonical_url = ? ORDER BY e.published_at DESC LIMIT 1`,
			accountID, canonicalURL).Scan(&entryID)
		if err == nil {
			return entryID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	if guid != "" && feedRemoteID != "" {
		var entryID string
		err := db.QueryRowContext(ctx, `SELECT e.id FROM entries e
			JOIN sync_mappings m ON m.account_id = ? AND m.local_kind = 'feed'
				AND m.local_id = e.feed_id AND m.remote_id = ?
			WHERE e.guid = ? LIMIT 1`, accountID, feedRemoteID, guid).Scan(&entryID)
		if err == nil {
			return entryID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	return "", ErrNotFound
}

func ListSyncStateChanges(ctx context.Context, db *sql.DB, accountID string, since time.Time) ([]SyncStateChange, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT es.entry_id, m.remote_id, es.is_read, es.is_starred, es.updated_at
		FROM entry_states es
		JOIN sync_accounts a ON a.profile_id = es.profile_id AND a.id = ?
		JOIN sync_mappings m ON m.account_id = a.id AND m.local_kind = 'entry' AND m.local_id = es.entry_id
		WHERE es.updated_at > ? ORDER BY es.updated_at, es.entry_id`, accountID, formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("list sync state changes: %w", err)
	}
	defer rows.Close()
	items := make([]SyncStateChange, 0)
	for rows.Next() {
		var item SyncStateChange
		var read, starred int
		var updatedAt string
		if err := rows.Scan(&item.LocalEntryID, &item.RemoteID, &read, &starred, &updatedAt); err != nil {
			return nil, err
		}
		item.Read = read == 1
		item.Starred = starred == 1
		item.UpdatedAt = parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func EntryStateChangedAfter(ctx context.Context, db *sql.DB, profileID, entryID string, since time.Time) (bool, error) {
	var changed bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM entry_states WHERE profile_id = ? AND entry_id = ? AND updated_at > ?
	)`, profileID, entryID, formatTime(since)).Scan(&changed)
	if err != nil {
		return false, fmt.Errorf("check local sync conflict: %w", err)
	}
	return changed, nil
}

func scanSyncAccount(row scanner) (domain.SyncAccount, error) {
	var account domain.SyncAccount
	var enabled, allowPrivate int
	var lastSync, nextSync, lastAttempt, errorCode, errorMessage sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&account.ID, &account.Provider, &account.Name, &account.Endpoint, &enabled,
		&allowPrivate, &account.SyncIntervalMinutes, &lastSync, &nextSync,
		&lastAttempt, &errorCode, &errorMessage, &createdAt, &updatedAt,
	); err != nil {
		return domain.SyncAccount{}, err
	}
	account.Enabled = enabled == 1
	account.AllowPrivateNetwork = allowPrivate == 1
	account.LastSyncAt = timePointer(lastSync)
	account.NextSyncAt = timePointer(nextSync)
	account.LastAttemptAt = timePointer(lastAttempt)
	account.LastErrorCode = stringPointer(errorCode)
	account.LastErrorMessage = stringPointer(errorMessage)
	account.CreatedAt = parseTime(createdAt)
	account.UpdatedAt = parseTime(updatedAt)
	return account, nil
}
