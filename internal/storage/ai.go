package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

const aiProfileColumns = `
	id, provider, name, endpoint, model, enabled, allow_private_network,
	remote_content_approved, is_default, last_used_at, last_error_code,
	last_error_message, created_at, updated_at`

type AIProfileRecord struct {
	Profile         domain.AIProfile
	ProfileID       string
	EncryptedAPIKey []byte
	SettingsJSON    string
}

type CreateAIProfileParams struct {
	ID                    string
	ProfileID             string
	Provider              string
	Name                  string
	Endpoint              string
	Model                 string
	EncryptedAPIKey       []byte
	SettingsJSON          string
	Enabled               bool
	AllowPrivateNetwork   bool
	RemoteContentApproved bool
	IsDefault             bool
}

type AIProfilePatch struct {
	Name                  *string
	Endpoint              *string
	Model                 *string
	EncryptedAPIKey       []byte
	SetEncryptedAPIKey    bool
	SettingsJSON          *string
	Enabled               *bool
	AllowPrivateNetwork   *bool
	RemoteContentApproved *bool
	IsDefault             *bool
}

type AIEntryContent struct {
	EntryID      string
	Title        string
	FeedTitle    string
	PublishedAt  string
	CanonicalURL string
	Content      string
}

type SaveAIResultParams struct {
	ProfileID   string
	AIProfileID string
	EntryID     string
	JobID       string
	Operation   string
	Language    string
	InputHash   string
	ResultText  string
	Provider    string
	Model       string
	Usage       domain.AIUsage
}

func CreateAIProfile(ctx context.Context, db *sql.DB, params CreateAIProfileParams) (domain.AIProfile, error) {
	if params.ID == "" {
		params.ID = uuid.NewString()
	}
	if params.ProfileID == "" {
		params.ProfileID = domain.DefaultProfileID
	}
	if params.SettingsJSON == "" {
		params.SettingsJSON = "{}"
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AIProfile{}, fmt.Errorf("begin AI profile create: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_profiles WHERE profile_id = ?", params.ProfileID).Scan(&count); err != nil {
		return domain.AIProfile{}, err
	}
	if count == 0 {
		params.IsDefault = true
	}
	if params.IsDefault {
		if _, err := tx.ExecContext(ctx, "UPDATE ai_profiles SET is_default = 0 WHERE profile_id = ?", params.ProfileID); err != nil {
			return domain.AIProfile{}, err
		}
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ai_profiles (
			id, profile_id, provider, name, endpoint, model, encrypted_api_key,
			settings_json, is_default, enabled, allow_private_network,
			remote_content_approved, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		params.ID, params.ProfileID, params.Provider, params.Name, params.Endpoint, params.Model,
		nullableBytes(params.EncryptedAPIKey), params.SettingsJSON, boolInt(params.IsDefault),
		boolInt(params.Enabled), boolInt(params.AllowPrivateNetwork), boolInt(params.RemoteContentApproved),
		formatTime(now), formatTime(now),
	)
	if err != nil {
		return domain.AIProfile{}, fmt.Errorf("create AI profile: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.AIProfile{}, err
	}
	record, err := GetAIProfileRecord(ctx, db, params.ProfileID, params.ID)
	return record.Profile, err
}

func ListAIProfiles(ctx context.Context, db *sql.DB, profileID string) ([]domain.AIProfile, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+aiProfileColumns+` FROM ai_profiles
		WHERE profile_id = ? ORDER BY is_default DESC, name COLLATE NOCASE, id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list AI profiles: %w", err)
	}
	defer rows.Close()
	items := make([]domain.AIProfile, 0)
	for rows.Next() {
		item, scanErr := scanAIProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func GetAIProfileRecord(ctx context.Context, db *sql.DB, profileID, aiProfileID string) (AIProfileRecord, error) {
	row := db.QueryRowContext(ctx, `SELECT `+aiProfileColumns+`, profile_id,
		encrypted_api_key, settings_json FROM ai_profiles WHERE profile_id = ? AND id = ?`, profileID, aiProfileID)
	return scanAIProfileRecord(row)
}

func GetDefaultAIProfileRecord(ctx context.Context, db *sql.DB, profileID string) (AIProfileRecord, error) {
	row := db.QueryRowContext(ctx, `SELECT `+aiProfileColumns+`, profile_id,
		encrypted_api_key, settings_json FROM ai_profiles
		WHERE profile_id = ? AND enabled = 1 ORDER BY is_default DESC, created_at, id LIMIT 1`, profileID)
	return scanAIProfileRecord(row)
}

func UpdateAIProfile(ctx context.Context, db *sql.DB, profileID, aiProfileID string, patch AIProfilePatch) (domain.AIProfile, error) {
	record, err := GetAIProfileRecord(ctx, db, profileID, aiProfileID)
	if err != nil {
		return domain.AIProfile{}, err
	}
	name, endpoint, model := record.Profile.Name, record.Profile.Endpoint, record.Profile.Model
	key, settings := record.EncryptedAPIKey, record.SettingsJSON
	enabled, allowPrivate := record.Profile.Enabled, record.Profile.AllowPrivateNetwork
	approved, isDefault := record.Profile.RemoteContentApproved, record.Profile.IsDefault
	if patch.Name != nil {
		name = *patch.Name
	}
	if patch.Endpoint != nil {
		endpoint = *patch.Endpoint
	}
	if patch.Model != nil {
		model = *patch.Model
	}
	if patch.SetEncryptedAPIKey {
		key = patch.EncryptedAPIKey
	}
	if patch.SettingsJSON != nil {
		settings = *patch.SettingsJSON
	}
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	if patch.AllowPrivateNetwork != nil {
		allowPrivate = *patch.AllowPrivateNetwork
	}
	if patch.RemoteContentApproved != nil {
		approved = *patch.RemoteContentApproved
	}
	if patch.IsDefault != nil {
		isDefault = *patch.IsDefault
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AIProfile{}, err
	}
	defer tx.Rollback()
	if isDefault {
		if _, err := tx.ExecContext(ctx, "UPDATE ai_profiles SET is_default = 0 WHERE profile_id = ?", profileID); err != nil {
			return domain.AIProfile{}, err
		}
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE ai_profiles SET
		name = ?, endpoint = ?, model = ?, encrypted_api_key = ?, settings_json = ?,
		enabled = ?, allow_private_network = ?, remote_content_approved = ?,
		is_default = ?, updated_at = ? WHERE profile_id = ? AND id = ?`,
		name, endpoint, model, nullableBytes(key), settings, boolInt(enabled), boolInt(allowPrivate),
		boolInt(approved), boolInt(isDefault), formatTime(now), profileID, aiProfileID)
	if err != nil {
		return domain.AIProfile{}, fmt.Errorf("update AI profile: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return domain.AIProfile{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return domain.AIProfile{}, err
	}
	updated, err := GetAIProfileRecord(ctx, db, profileID, aiProfileID)
	return updated.Profile, err
}

func DeleteAIProfile(ctx context.Context, db *sql.DB, profileID, aiProfileID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var wasDefault bool
	if err := tx.QueryRowContext(ctx, "SELECT is_default FROM ai_profiles WHERE profile_id = ? AND id = ?", profileID, aiProfileID).Scan(&wasDefault); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM ai_profiles WHERE profile_id = ? AND id = ?", profileID, aiProfileID); err != nil {
		return fmt.Errorf("delete AI profile: %w", err)
	}
	if wasDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_profiles SET is_default = 1
			WHERE id = (SELECT id FROM ai_profiles WHERE profile_id = ? ORDER BY created_at, id LIMIT 1)`, profileID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func MarkAIProfileSuccess(ctx context.Context, db *sql.DB, aiProfileID string, usedAt time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE ai_profiles SET last_used_at = ?, last_error_code = NULL,
		last_error_message = NULL, updated_at = ? WHERE id = ?`, formatTime(usedAt), formatTime(usedAt), aiProfileID)
	return err
}

func MarkAIProfileFailure(ctx context.Context, db *sql.DB, aiProfileID, code, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err := db.ExecContext(ctx, `UPDATE ai_profiles SET last_error_code = ?, last_error_message = ?,
		updated_at = ? WHERE id = ?`, code, message, formatTime(time.Now().UTC()), aiProfileID)
	return err
}

func GetAIEntryContent(ctx context.Context, db *sql.DB, profileID, entryID string) (AIEntryContent, error) {
	var item AIEntryContent
	var canonicalURL sql.NullString
	err := db.QueryRowContext(ctx, `SELECT e.id, e.title, COALESCE(s.title_override, f.title), e.published_at, e.canonical_url,
			COALESCE(NULLIF(ec.readability_text, ''), NULLIF(ec.plain_text, ''), NULLIF(e.summary, ''), e.title)
			FROM entries e
			JOIN feeds f ON f.id = e.feed_id
			JOIN subscriptions s ON s.feed_id = e.feed_id AND s.profile_id = ?
			LEFT JOIN entry_contents ec ON ec.entry_id = e.id WHERE e.id = ?`, profileID, entryID,
	).Scan(&item.EntryID, &item.Title, &item.FeedTitle, &item.PublishedAt, &canonicalURL, &item.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return AIEntryContent{}, ErrNotFound
	}
	if err != nil {
		return AIEntryContent{}, fmt.Errorf("get AI entry content: %w", err)
	}
	if canonicalURL.Valid {
		item.CanonicalURL = canonicalURL.String
	}
	return item, nil
}

func GetAIResult(ctx context.Context, db *sql.DB, profileID, entryID, operation, language, inputHash string) (domain.AIResult, error) {
	item, err := scanAIResult(db.QueryRowContext(ctx, `SELECT id, ai_profile_id, entry_id, operation,
		language, input_hash, result_text, usage_json, created_at FROM ai_results
		WHERE profile_id = ? AND entry_id = ? AND operation = ? AND language = ? AND input_hash = ?`,
		profileID, entryID, operation, language, inputHash))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AIResult{}, ErrNotFound
	}
	return item, err
}

func ListAIResults(ctx context.Context, db *sql.DB, profileID, entryID string) ([]domain.AIResult, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, ai_profile_id, entry_id, operation,
		language, input_hash, result_text, usage_json, created_at FROM ai_results
		WHERE profile_id = ? AND entry_id = ? ORDER BY created_at DESC`, profileID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.AIResult, 0)
	for rows.Next() {
		item, scanErr := scanAIResult(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func SaveAIResultAndUsage(ctx context.Context, db *sql.DB, params SaveAIResultParams) (domain.AIResult, bool, error) {
	usageBody, err := json.Marshal(params.Usage)
	if err != nil {
		return domain.AIResult{}, false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AIResult{}, false, err
	}
	defer tx.Rollback()
	resultID := uuid.NewString()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ai_results (
		id, profile_id, ai_profile_id, entry_id, operation, language, input_hash,
		result_text, usage_json, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, resultID, params.ProfileID, params.AIProfileID,
		params.EntryID, params.Operation, params.Language, params.InputHash, params.ResultText,
		string(usageBody), formatTime(now))
	if err != nil {
		return domain.AIResult{}, false, err
	}
	inserted, _ := result.RowsAffected()
	if inserted > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_usage (
			id, profile_id, ai_profile_id, entry_id, job_id, operation, provider, model,
			input_tokens, output_tokens, total_tokens, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uuid.NewString(), params.ProfileID,
			params.AIProfileID, params.EntryID, nullableStringValue(params.JobID), params.Operation,
			params.Provider, params.Model, params.Usage.InputTokens, params.Usage.OutputTokens,
			params.Usage.TotalTokens, formatTime(now))
		if err != nil {
			return domain.AIResult{}, false, err
		}
	}
	item, err := scanAIResult(tx.QueryRowContext(ctx, `SELECT id, ai_profile_id, entry_id, operation,
		language, input_hash, result_text, usage_json, created_at FROM ai_results
		WHERE profile_id = ? AND entry_id = ? AND operation = ? AND language = ? AND input_hash = ?`,
		params.ProfileID, params.EntryID, params.Operation, params.Language, params.InputHash))
	if err != nil {
		return domain.AIResult{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AIResult{}, false, err
	}
	return item, inserted > 0, nil
}

func CreateAIChatSession(ctx context.Context, db *sql.DB, profileID, aiProfileID, entryID, title string) (domain.AIChatSession, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `INSERT INTO ai_chat_sessions
		(id, profile_id, ai_profile_id, entry_id, title, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, profileID, aiProfileID, nullableStringValue(entryID), title, formatTime(now), formatTime(now))
	if err != nil {
		return domain.AIChatSession{}, fmt.Errorf("create AI chat: %w", err)
	}
	return GetAIChatSession(ctx, db, profileID, id)
}

func GetAIChatSession(ctx context.Context, db *sql.DB, profileID, sessionID string) (domain.AIChatSession, error) {
	var session domain.AIChatSession
	var aiProfileID, entryID sql.NullString
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx, `SELECT id, ai_profile_id, entry_id, title, created_at, updated_at
		FROM ai_chat_sessions WHERE profile_id = ? AND id = ?`, profileID, sessionID,
	).Scan(&session.ID, &aiProfileID, &entryID, &session.Title, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AIChatSession{}, ErrNotFound
	}
	if err != nil {
		return domain.AIChatSession{}, err
	}
	session.AIProfileID = stringPointer(aiProfileID)
	session.EntryID = stringPointer(entryID)
	session.CreatedAt, session.UpdatedAt = parseTime(createdAt), parseTime(updatedAt)
	rows, err := db.QueryContext(ctx, `SELECT id, role, content, metadata_json, status, usage_json, created_at
		FROM ai_chat_messages WHERE session_id = ? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return domain.AIChatSession{}, err
	}
	defer rows.Close()
	session.Messages = make([]domain.AIChatMessage, 0)
	for rows.Next() {
		var message domain.AIChatMessage
		var metadata, usage, created string
		if err := rows.Scan(&message.ID, &message.Role, &message.Content, &metadata, &message.Status, &usage, &created); err != nil {
			return domain.AIChatSession{}, err
		}
		message.Metadata, message.Usage = json.RawMessage(metadata), json.RawMessage(usage)
		message.CreatedAt = parseTime(created)
		session.Messages = append(session.Messages, message)
	}
	return session, rows.Err()
}

func AddAIChatMessage(ctx context.Context, db *sql.DB, sessionID, role, content, status, jobID string, metadata, usage json.RawMessage) (domain.AIChatMessage, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `INSERT INTO ai_chat_messages
		(id, session_id, role, content, metadata_json, status, job_id, usage_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, sessionID, role, content, string(metadata), status,
		nullableStringValue(jobID), string(usage), formatTime(now))
	if err != nil {
		return domain.AIChatMessage{}, fmt.Errorf("add AI chat message: %w", err)
	}
	_, _ = db.ExecContext(ctx, "UPDATE ai_chat_sessions SET updated_at = ? WHERE id = ?", formatTime(now), sessionID)
	return domain.AIChatMessage{ID: id, Role: role, Content: content, Metadata: metadata, Status: status, Usage: usage, CreatedAt: now}, nil
}

func AIChatAssistantExistsForJob(ctx context.Context, db *sql.DB, jobID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_chat_messages
		WHERE job_id = ? AND role = 'assistant')`, jobID).Scan(&exists)
	return exists, err
}

func SaveAIChatAssistantAndUsage(ctx context.Context, db *sql.DB, profileID, aiProfileID, entryID, sessionID, jobID, provider, model, content string, usage domain.AIUsage) (domain.AIChatMessage, error) {
	usageBody, err := json.Marshal(usage)
	if err != nil {
		return domain.AIChatMessage{}, err
	}
	metadataBody, err := json.Marshal(map[string]string{"provider": provider, "model": model})
	if err != nil {
		return domain.AIChatMessage{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AIChatMessage{}, err
	}
	defer tx.Rollback()
	id := uuid.NewString()
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_messages
		(id, session_id, role, content, metadata_json, status, job_id, usage_json, created_at)
		VALUES (?, ?, 'assistant', ?, ?, 'completed', ?, ?, ?)`, id, sessionID, content,
		string(metadataBody), jobID, string(usageBody), formatTime(now))
	if err != nil {
		return domain.AIChatMessage{}, fmt.Errorf("save AI chat response: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_usage (
		id, profile_id, ai_profile_id, entry_id, job_id, operation, provider, model,
		input_tokens, output_tokens, total_tokens, created_at
	) VALUES (?, ?, ?, ?, ?, 'chat', ?, ?, ?, ?, ?, ?)`, uuid.NewString(), profileID,
		aiProfileID, nullableStringValue(entryID), jobID, provider, model, usage.InputTokens, usage.OutputTokens,
		usage.TotalTokens, formatTime(now))
	if err != nil {
		return domain.AIChatMessage{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE ai_chat_sessions SET updated_at = ? WHERE id = ?", formatTime(now), sessionID); err != nil {
		return domain.AIChatMessage{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AIChatMessage{}, err
	}
	return domain.AIChatMessage{ID: id, Role: "assistant", Content: content, Metadata: metadataBody, Status: "completed", Usage: usageBody, CreatedAt: now}, nil
}

func GetAIUsageTotals(ctx context.Context, db *sql.DB, profileID string) (domain.AIUsage, error) {
	var usage domain.AIUsage
	err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM ai_usage WHERE profile_id = ?`, profileID).Scan(&usage.InputTokens, &usage.OutputTokens, &usage.TotalTokens)
	return usage, err
}

func scanAIProfile(row scanner) (domain.AIProfile, error) {
	var item domain.AIProfile
	var enabled, allowPrivate, approved, isDefault int
	var lastUsed, errorCode, errorMessage sql.NullString
	var created, updated string
	err := row.Scan(&item.ID, &item.Provider, &item.Name, &item.Endpoint, &item.Model,
		&enabled, &allowPrivate, &approved, &isDefault, &lastUsed, &errorCode, &errorMessage,
		&created, &updated)
	if err != nil {
		return domain.AIProfile{}, err
	}
	item.Enabled, item.AllowPrivateNetwork = enabled == 1, allowPrivate == 1
	item.RemoteContentApproved, item.IsDefault = approved == 1, isDefault == 1
	item.LastUsedAt, item.LastErrorCode, item.LastErrorMessage = timePointer(lastUsed), stringPointer(errorCode), stringPointer(errorMessage)
	item.CreatedAt, item.UpdatedAt = parseTime(created), parseTime(updated)
	return item, nil
}

func scanAIProfileRecord(row scanner) (AIProfileRecord, error) {
	var record AIProfileRecord
	var enabled, allowPrivate, approved, isDefault int
	var lastUsed, errorCode, errorMessage sql.NullString
	var encryptedKey []byte
	var created, updated string
	err := row.Scan(&record.Profile.ID, &record.Profile.Provider, &record.Profile.Name,
		&record.Profile.Endpoint, &record.Profile.Model, &enabled, &allowPrivate, &approved,
		&isDefault, &lastUsed, &errorCode, &errorMessage, &created, &updated,
		&record.ProfileID, &encryptedKey, &record.SettingsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return AIProfileRecord{}, ErrNotFound
	}
	if err != nil {
		return AIProfileRecord{}, err
	}
	record.Profile.Enabled, record.Profile.AllowPrivateNetwork = enabled == 1, allowPrivate == 1
	record.Profile.RemoteContentApproved, record.Profile.IsDefault = approved == 1, isDefault == 1
	record.Profile.LastUsedAt, record.Profile.LastErrorCode, record.Profile.LastErrorMessage = timePointer(lastUsed), stringPointer(errorCode), stringPointer(errorMessage)
	record.Profile.CreatedAt, record.Profile.UpdatedAt = parseTime(created), parseTime(updated)
	record.EncryptedAPIKey = encryptedKey
	return record, nil
}

func scanAIResult(row scanner) (domain.AIResult, error) {
	var item domain.AIResult
	var aiProfileID sql.NullString
	var usage, created string
	if err := row.Scan(&item.ID, &aiProfileID, &item.EntryID, &item.Operation, &item.Language,
		&item.InputHash, &item.ResultText, &usage, &created); err != nil {
		return domain.AIResult{}, err
	}
	item.AIProfileID = stringPointer(aiProfileID)
	item.Usage, item.CreatedAt = json.RawMessage(usage), parseTime(created)
	return item, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableStringValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}
