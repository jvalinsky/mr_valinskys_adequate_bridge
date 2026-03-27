package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	conn *sql.DB
}

type BridgedAccount struct {
	ATDID     string
	SSBFeedID string
	CreatedAt time.Time
	Active    bool
}

type Message struct {
	ATURI                string
	ATCID                string
	SSBMsgRef            string
	ATDID                string
	Type                 string
	RawATJson            string
	RawSSBJson           string
	PublishedAt          *time.Time
	PublishError         string
	PublishAttempts      int
	LastPublishAttemptAt *time.Time
	CreatedAt            time.Time
}

type Blob struct {
	ATCID        string
	SSBBlobRef   string
	Size         int64
	MimeType     string
	DownloadedAt time.Time
}

type BridgeState struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

func Open(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) initSchema() error {
	if _, err := db.conn.Exec(schemaSQL); err != nil {
		return err
	}

	// Migration-safe column adds for pre-existing databases created before
	// publish metadata existed.
	if err := db.ensureColumn("messages", "published_at", "DATETIME"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "publish_error", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "publish_attempts", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "last_publish_attempt_at", "DATETIME"); err != nil {
		return err
	}

	return nil
}

func (db *DB) ensureColumn(table, column, definition string) error {
	exists, err := db.columnExists(table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = db.conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func (db *DB) columnExists(table, column string) (bool, error) {
	rows, err := db.conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (db *DB) AddBridgedAccount(ctx context.Context, acc BridgedAccount) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO bridged_accounts (at_did, ssb_feed_id, active) VALUES (?, ?, ?)
		 ON CONFLICT(at_did) DO UPDATE SET ssb_feed_id=excluded.ssb_feed_id, active=excluded.active`,
		acc.ATDID, acc.SSBFeedID, acc.Active,
	)
	return err
}

func (db *DB) GetBridgedAccount(ctx context.Context, atDID string) (*BridgedAccount, error) {
	var acc BridgedAccount
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts WHERE at_did = ?`,
		atDID,
	).Scan(&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &acc, nil
}

func (db *DB) GetAllBridgedAccounts(ctx context.Context) ([]BridgedAccount, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []BridgedAccount
	for rows.Next() {
		var acc BridgedAccount
		if err := rows.Scan(&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

func (db *DB) CountBridgedAccounts(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridged_accounts`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) CountActiveBridgedAccounts(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridged_accounts WHERE active = 1`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) AddMessage(ctx context.Context, msg Message) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO messages (
			at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at
		)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(at_uri) DO UPDATE SET
		 	at_cid=excluded.at_cid,
		 	ssb_msg_ref=excluded.ssb_msg_ref,
		 	at_did=excluded.at_did,
		 	type=excluded.type,
		 	raw_at_json=excluded.raw_at_json,
		 	raw_ssb_json=excluded.raw_ssb_json,
		 	published_at=excluded.published_at,
		 	publish_error=excluded.publish_error,
		 	publish_attempts=messages.publish_attempts + excluded.publish_attempts,
		 	last_publish_attempt_at=excluded.last_publish_attempt_at`,
		msg.ATURI,
		msg.ATCID,
		msg.SSBMsgRef,
		msg.ATDID,
		msg.Type,
		msg.RawATJson,
		msg.RawSSBJson,
		msg.PublishedAt,
		msg.PublishError,
		msg.PublishAttempts,
		msg.LastPublishAttemptAt,
	)
	return err
}

func (db *DB) GetMessage(ctx context.Context, atURI string) (*Message, error) {
	var msg Message
	var ssbMsgRef, rawATJson, rawSSBJson, publishError sql.NullString
	var publishedAt, lastPublishAttemptAt sql.NullTime
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, created_at
		 FROM messages
		 WHERE at_uri = ?`,
		atURI,
	).Scan(
		&msg.ATURI,
		&msg.ATCID,
		&ssbMsgRef,
		&msg.ATDID,
		&msg.Type,
		&rawATJson,
		&rawSSBJson,
		&publishedAt,
		&publishError,
		&msg.PublishAttempts,
		&lastPublishAttemptAt,
		&msg.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	msg.SSBMsgRef = ssbMsgRef.String
	msg.RawATJson = rawATJson.String
	msg.RawSSBJson = rawSSBJson.String
	msg.PublishError = publishError.String
	if publishedAt.Valid {
		t := publishedAt.Time
		msg.PublishedAt = &t
	}
	if lastPublishAttemptAt.Valid {
		t := lastPublishAttemptAt.Time
		msg.LastPublishAttemptAt = &t
	}
	return &msg, nil
}

func (db *DB) CountMessages(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) GetRecentMessages(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, created_at
		 FROM messages
		 ORDER BY created_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var ssbMsgRef, rawATJson, rawSSBJson, publishError sql.NullString
		var publishedAt, lastPublishAttemptAt sql.NullTime
		if err := rows.Scan(
			&msg.ATURI,
			&msg.ATCID,
			&ssbMsgRef,
			&msg.ATDID,
			&msg.Type,
			&rawATJson,
			&rawSSBJson,
			&publishedAt,
			&publishError,
			&msg.PublishAttempts,
			&lastPublishAttemptAt,
			&msg.CreatedAt,
		); err != nil {
			return nil, err
		}
		msg.SSBMsgRef = ssbMsgRef.String
		msg.RawATJson = rawATJson.String
		msg.RawSSBJson = rawSSBJson.String
		msg.PublishError = publishError.String
		if publishedAt.Valid {
			t := publishedAt.Time
			msg.PublishedAt = &t
		}
		if lastPublishAttemptAt.Valid {
			t := lastPublishAttemptAt.Time
			msg.LastPublishAttemptAt = &t
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (db *DB) CountPublishedMessages(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> ''`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) CountPublishFailures(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE publish_error IS NOT NULL AND publish_error <> ''`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) GetPublishFailures(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, created_at
		 FROM messages
		 WHERE publish_error IS NOT NULL AND publish_error <> ''
		 ORDER BY created_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var ssbMsgRef, rawATJson, rawSSBJson, publishError sql.NullString
		var publishedAt, lastPublishAttemptAt sql.NullTime
		if err := rows.Scan(
			&msg.ATURI,
			&msg.ATCID,
			&ssbMsgRef,
			&msg.ATDID,
			&msg.Type,
			&rawATJson,
			&rawSSBJson,
			&publishedAt,
			&publishError,
			&msg.PublishAttempts,
			&lastPublishAttemptAt,
			&msg.CreatedAt,
		); err != nil {
			return nil, err
		}
		msg.SSBMsgRef = ssbMsgRef.String
		msg.RawATJson = rawATJson.String
		msg.RawSSBJson = rawSSBJson.String
		msg.PublishError = publishError.String
		if publishedAt.Valid {
			t := publishedAt.Time
			msg.PublishedAt = &t
		}
		if lastPublishAttemptAt.Valid {
			t := lastPublishAttemptAt.Time
			msg.LastPublishAttemptAt = &t
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (db *DB) GetRetryCandidates(ctx context.Context, limit int, atDID string, maxAttempts int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}

	var query strings.Builder
	query.WriteString(
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, created_at
		 FROM messages
		 WHERE (ssb_msg_ref IS NULL OR ssb_msg_ref = '')
		   AND publish_error IS NOT NULL AND publish_error <> ''
		   AND publish_attempts < ?`,
	)

	args := []interface{}{maxAttempts}
	if strings.TrimSpace(atDID) != "" {
		query.WriteString(" AND at_did = ?")
		args = append(args, strings.TrimSpace(atDID))
	}
	query.WriteString(" ORDER BY COALESCE(last_publish_attempt_at, created_at) ASC LIMIT ?")
	args = append(args, limit)

	rows, err := db.conn.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var ssbMsgRef, rawATJson, rawSSBJson, publishError sql.NullString
		var publishedAt, lastPublishAttemptAt sql.NullTime
		if err := rows.Scan(
			&msg.ATURI,
			&msg.ATCID,
			&ssbMsgRef,
			&msg.ATDID,
			&msg.Type,
			&rawATJson,
			&rawSSBJson,
			&publishedAt,
			&publishError,
			&msg.PublishAttempts,
			&lastPublishAttemptAt,
			&msg.CreatedAt,
		); err != nil {
			return nil, err
		}
		msg.SSBMsgRef = ssbMsgRef.String
		msg.RawATJson = rawATJson.String
		msg.RawSSBJson = rawSSBJson.String
		msg.PublishError = publishError.String
		if publishedAt.Valid {
			t := publishedAt.Time
			msg.PublishedAt = &t
		}
		if lastPublishAttemptAt.Valid {
			t := lastPublishAttemptAt.Time
			msg.LastPublishAttemptAt = &t
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func (db *DB) AddBlob(ctx context.Context, blob Blob) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO blobs (at_cid, ssb_blob_ref, size, mime_type)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(at_cid) DO UPDATE SET
		 	ssb_blob_ref=excluded.ssb_blob_ref,
		 	size=excluded.size,
		 	mime_type=excluded.mime_type,
		 	downloaded_at=CURRENT_TIMESTAMP`,
		blob.ATCID, blob.SSBBlobRef, blob.Size, blob.MimeType,
	)
	return err
}

func (db *DB) GetBlob(ctx context.Context, atCID string) (*Blob, error) {
	var blob Blob
	var mimeType sql.NullString
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_cid, ssb_blob_ref, COALESCE(size, 0), mime_type, downloaded_at
		 FROM blobs
		 WHERE at_cid = ?`,
		atCID,
	).Scan(&blob.ATCID, &blob.SSBBlobRef, &blob.Size, &mimeType, &blob.DownloadedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	blob.MimeType = mimeType.String
	return &blob, nil
}

func (db *DB) CountBlobs(ctx context.Context) (int, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM blobs`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (db *DB) GetRecentBlobs(ctx context.Context, limit int) ([]Blob, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_cid, ssb_blob_ref, COALESCE(size, 0), mime_type, downloaded_at
		 FROM blobs
		 ORDER BY downloaded_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []Blob
	for rows.Next() {
		var blob Blob
		var mimeType sql.NullString
		if err := rows.Scan(&blob.ATCID, &blob.SSBBlobRef, &blob.Size, &mimeType, &blob.DownloadedAt); err != nil {
			return nil, err
		}
		blob.MimeType = mimeType.String
		blobs = append(blobs, blob)
	}

	return blobs, nil
}

func (db *DB) SetBridgeState(ctx context.Context, key, value string) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO bridge_state (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
		key,
		value,
	)
	return err
}

func (db *DB) GetBridgeState(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := db.conn.QueryRowContext(ctx, `SELECT value FROM bridge_state WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (db *DB) GetAllBridgeState(ctx context.Context) ([]BridgeState, error) {
	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT key, value, updated_at
		 FROM bridge_state
		 ORDER BY key ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var state []BridgeState
	for rows.Next() {
		var s BridgeState
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, err
		}
		state = append(state, s)
	}

	return state, nil
}
