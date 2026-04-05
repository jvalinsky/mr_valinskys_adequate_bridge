// Package db provides SQLite-backed persistence for bridge state and mappings.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a SQLite connection used by bridge components.
type DB struct {
	conn *sql.DB
}

// BridgedAccount stores the DID-to-SSB identity mapping and activation status.
type BridgedAccount struct {
	ATDID     string
	SSBFeedID string
	CreatedAt time.Time
	Active    bool
}

// BridgedAccountStats extends BridgedAccount with per-bot message statistics.
type BridgedAccountStats struct {
	BridgedAccount
	TotalMessages     int
	PublishedMessages int
	FailedMessages    int
	DeferredMessages  int
	LastPublishedAt   *time.Time
}

// Message stores one bridged record and publish lifecycle metadata.
type Message struct {
	ATURI                string
	ATCID                string
	SSBMsgRef            string
	ATDID                string
	Type                 string
	MessageState         string
	RawATJson            string
	RawSSBJson           string
	PublishedAt          *time.Time
	PublishError         string
	PublishAttempts      int
	LastPublishAttemptAt *time.Time
	DeferReason          string
	DeferAttempts        int
	LastDeferAttemptAt   *time.Time
	DeletedAt            *time.Time
	DeletedSeq           *int64
	DeletedReason        string
	CreatedAt            time.Time
}

// MessageListQuery controls filtered browsing in the admin UI.
type MessageListQuery struct {
	Search    string
	Type      string
	State     string
	Sort      string
	Limit     int
	ATDID     string
	HasIssue  bool
	Cursor    string
	Direction string
}

// MessagePage is one paginated message-list result for the admin UI.
type MessagePage struct {
	Messages   []Message
	HasNext    bool
	HasPrev    bool
	NextCursor string
	PrevCursor string
}

// DeferredReasonCount is one aggregated deferred-reason bucket.
type DeferredReasonCount struct {
	Reason string
	Count  int
}

// AccountIssueSummary is one aggregated account-level issue summary.
type AccountIssueSummary struct {
	ATDID          string
	SSBFeedID      string
	Active         bool
	TotalMessages  int
	IssueMessages  int
	FailedMessages int
	DeferredCount  int
	DeletedCount   int
}

// Blob stores one ATProto CID to SSB blob reference mapping.
type Blob struct {
	ATCID        string
	SSBBlobRef   string
	Size         int64
	MimeType     string
	DownloadedAt time.Time
}

// BridgeState stores small key/value runtime state such as cursors.
type BridgeState struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

const (
	MessageStatePending   = "pending"
	MessageStatePublished = "published"
	MessageStateFailed    = "failed"
	MessageStateDeferred  = "deferred"
	MessageStateDeleted   = "deleted"
)

// Open opens (and initializes) the bridge database at dbPath.
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

// Close closes the underlying SQLite connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// querySlice executes a query and scans each row into T using the provided
// scan function. It handles rows.Close and rows.Err automatically.
func querySlice[T any](ctx context.Context, conn *sql.DB, op, query string, args []any, scan func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var result []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return result, nil
}

func (db *DB) initSchema() error {
	migrations, err := loadMigrations("internal/db/migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	if err := db.runMigrations(migrations); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	if err := db.ensureLegacyColumns(); err != nil {
		return fmt.Errorf("ensure legacy columns: %w", err)
	}

	return nil
}

func (db *DB) ensureLegacyColumns() error {
	legacyColumns := map[string]map[string]string{
		"messages": {
			"published_at":            "DATETIME",
			"publish_error":           "TEXT",
			"publish_attempts":        "INTEGER NOT NULL DEFAULT 0",
			"last_publish_attempt_at": "DATETIME",
			"message_state":           "TEXT NOT NULL DEFAULT 'pending'",
			"defer_reason":            "TEXT",
			"defer_attempts":          "INTEGER NOT NULL DEFAULT 0",
			"last_defer_attempt_at":   "DATETIME",
			"deleted_at":              "DATETIME",
			"deleted_seq":             "INTEGER",
			"deleted_reason":          "TEXT",
		},
	}

	for table, columns := range legacyColumns {
		for column, definition := range columns {
			if err := db.ensureColumn(table, column, definition); err != nil {
				return err
			}
		}
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

// AddBlob inserts or updates one blob CID mapping.
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
	if err != nil {
		return fmt.Errorf("add blob %s: %w", blob.ATCID, err)
	}
	return nil
}

// GetBlob returns the blob row for atCID, or nil when absent.
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
		return nil, fmt.Errorf("get blob %s: %w", atCID, err)
	}
	blob.MimeType = mimeType.String
	return &blob, nil
}

// GetBlobBySSBRef returns the blob row for ssbBlobRef, or nil when absent.
func (db *DB) GetBlobBySSBRef(ctx context.Context, ssbBlobRef string) (*Blob, error) {
	var blob Blob
	var mimeType sql.NullString
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_cid, ssb_blob_ref, COALESCE(size, 0), mime_type, downloaded_at
		 FROM blobs
		 WHERE ssb_blob_ref = ?`,
		ssbBlobRef,
	).Scan(&blob.ATCID, &blob.SSBBlobRef, &blob.Size, &mimeType, &blob.DownloadedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get blob by ssb ref %s: %w", ssbBlobRef, err)
	}
	blob.MimeType = mimeType.String
	return &blob, nil
}

// CountBlobs returns the total number of bridged blobs.
func (db *DB) CountBlobs(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM blobs`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count blobs: %w", err)
	}
	return count, nil
}

// GetRecentBlobs returns the most recently downloaded blobs up to limit.
func (db *DB) GetRecentBlobs(ctx context.Context, limit int) ([]Blob, error) {
	if limit <= 0 {
		limit = config.DefaultBlobLimit
	}

	return querySlice(ctx, db.conn,
		"list recent blobs",
		`SELECT at_cid, ssb_blob_ref, COALESCE(size, 0), mime_type, downloaded_at
		 FROM blobs
		 ORDER BY downloaded_at DESC
		 LIMIT ?`,
		[]any{limit},
		scanBlobRow,
	)
}

// GetBlobsOlderThan returns blob mappings older than the specified days.
func (db *DB) GetBlobsOlderThan(ctx context.Context, days int) ([]Blob, error) {
	if days <= 0 {
		days = config.BlobMaxAgeDays
	}

	return querySlice(ctx, db.conn,
		"list old blobs",
		`SELECT at_cid, ssb_blob_ref, COALESCE(size, 0), mime_type, downloaded_at
		 FROM blobs
		 WHERE downloaded_at < datetime('now', '-' || ? || ' days')
		 ORDER BY downloaded_at ASC`,
		[]any{days},
		scanBlobRow,
	)
}

// CountBlobSize returns the total size of all blobs in bytes.
func (db *DB) CountBlobSize(ctx context.Context) (int64, error) {
	var size int64
	err := db.conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM blobs`).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("count blob size: %w", err)
	}
	return size, nil
}

// DeleteBlob removes a blob mapping by AT CID.
func (db *DB) DeleteBlob(ctx context.Context, atCID string) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM blobs WHERE at_cid = ?`, atCID)
	if err != nil {
		return fmt.Errorf("delete blob %s: %w", atCID, err)
	}
	return nil
}

// SetBridgeState upserts a key/value runtime state entry.
func (db *DB) SetBridgeState(ctx context.Context, key, value string) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO bridge_state (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
		key,
		value,
	)
	if err != nil {
		return fmt.Errorf("set bridge state %s: %w", key, err)
	}
	return nil
}

// GetBridgeState returns the value for key and whether it exists.
func (db *DB) GetBridgeState(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := db.conn.QueryRowContext(ctx, `SELECT value FROM bridge_state WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("get bridge state %s: %w", key, err)
	}
	return value, true, nil
}

// BridgeHealthStatus holds the result of a health check query.
type BridgeHealthStatus struct {
	Status        string // e.g. "live", "starting", "stopping", ""
	LastHeartbeat string // RFC3339 timestamp or ""
	Healthy       bool
}

// CheckBridgeHealth returns the bridge runtime health based on stored state.
// The bridge is healthy if its status is "live" and the last heartbeat is
// within the given staleness threshold.
func (db *DB) CheckBridgeHealth(ctx context.Context, maxStale time.Duration) (*BridgeHealthStatus, error) {
	result := &BridgeHealthStatus{}

	status, ok, err := db.GetBridgeState(ctx, "bridge_runtime_status")
	if err != nil {
		return nil, fmt.Errorf("check bridge health status: %w", err)
	}
	if ok {
		result.Status = status
	}

	heartbeat, ok, err := db.GetBridgeState(ctx, "bridge_runtime_last_heartbeat_at")
	if err != nil {
		return nil, fmt.Errorf("check bridge health heartbeat: %w", err)
	}
	if ok {
		result.LastHeartbeat = heartbeat
	}

	result.Healthy = result.Status == "live"
	if result.Healthy && result.LastHeartbeat != "" && maxStale > 0 {
		if t, err := time.Parse(time.RFC3339, result.LastHeartbeat); err == nil {
			if time.Since(t) > maxStale {
				result.Healthy = false
			}
		}
	}

	return result, nil
}

// GetAllBridgeState returns all runtime state entries sorted by key.
func (db *DB) GetAllBridgeState(ctx context.Context) ([]BridgeState, error) {
	return querySlice(ctx, db.conn,
		"list bridge state",
		`SELECT key, value, updated_at
		 FROM bridge_state
		 ORDER BY key ASC`,
		nil,
		scanBridgeStateRow,
	)
}

func scanBlobRow(rows *sql.Rows) (Blob, error) {
	var blob Blob
	var mimeType sql.NullString
	if err := rows.Scan(&blob.ATCID, &blob.SSBBlobRef, &blob.Size, &mimeType, &blob.DownloadedAt); err != nil {
		return Blob{}, err
	}
	blob.MimeType = mimeType.String
	return blob, nil
}

func scanBridgeStateRow(rows *sql.Rows) (BridgeState, error) {
	var state BridgeState
	if err := rows.Scan(&state.Key, &state.Value, &state.UpdatedAt); err != nil {
		return BridgeState{}, err
	}
	return state, nil
}

type FollowerSync struct {
	BotDID          string
	FollowerSSBFeed string
	FollowedBackAt  time.Time
}

func (db *DB) AddFollowerSync(ctx context.Context, botDID, followerSSBFeed string) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT OR IGNORE INTO ssb_follower_sync (bot_did, follower_ssb_feed)
		VALUES (?, ?)
	`, botDID, followerSSBFeed)
	return err
}

func (db *DB) HasFollowerSync(ctx context.Context, botDID, followerSSBFeed string) (bool, error) {
	var count int
	err := db.conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ssb_follower_sync
		WHERE bot_did = ? AND follower_ssb_feed = ?
	`, botDID, followerSSBFeed).Scan(&count)
	return count > 0, err
}

func (db *DB) ListFollowerSyncs(ctx context.Context, botDID string) ([]FollowerSync, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT bot_did, follower_ssb_feed, followed_back_at
		FROM ssb_follower_sync
		WHERE bot_did = ?
		ORDER BY followed_back_at DESC
	`, botDID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var syncs []FollowerSync
	for rows.Next() {
		var sync FollowerSync
		if err := rows.Scan(&sync.BotDID, &sync.FollowerSSBFeed, &sync.FollowedBackAt); err != nil {
			return nil, err
		}
		syncs = append(syncs, sync)
	}
	return syncs, rows.Err()
}

type DirectMessage struct {
	ID               int64
	SSBMsgKey        string
	SSBMsgSeq        int64
	SenderFeed       string
	RecipientFeed    string
	EncryptedContent []byte
	Plaintext        string
	DecryptedAt      time.Time
	CreatedAt        int64
	ReceivedAt       int64
	IsOutbound       bool
}

func (db *DB) SaveDM(msg *DirectMessage) error {
	_, err := db.conn.ExecContext(context.Background(), `
		INSERT OR REPLACE INTO direct_messages (
			ssb_msg_key, ssb_msg_seq, sender_feed, recipient_feed,
			encrypted_content, plaintext, decrypted_at, created_at, received_at, is_outbound
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		msg.SSBMsgKey,
		msg.SSBMsgSeq,
		msg.SenderFeed,
		msg.RecipientFeed,
		msg.EncryptedContent,
		nullString(msg.Plaintext),
		nullTime(msg.DecryptedAt),
		msg.CreatedAt,
		msg.ReceivedAt,
		msg.IsOutbound,
	)
	return err
}

func (db *DB) GetDMByKey(ctx context.Context, msgKey string) (*DirectMessage, error) {
	var dm DirectMessage
	var plaintext sql.NullString
	var decryptedAt sql.NullTime
	var ssbMsgSeq sql.NullInt64

	err := db.conn.QueryRowContext(ctx, `
		SELECT id, ssb_msg_key, ssb_msg_seq, sender_feed, recipient_feed,
			   encrypted_content, plaintext, decrypted_at, created_at, received_at, is_outbound
		FROM direct_messages
		WHERE ssb_msg_key = ?
	`, msgKey).Scan(
		&dm.ID,
		&dm.SSBMsgKey,
		&ssbMsgSeq,
		&dm.SenderFeed,
		&dm.RecipientFeed,
		&dm.EncryptedContent,
		&plaintext,
		&decryptedAt,
		&dm.CreatedAt,
		&dm.ReceivedAt,
		&dm.IsOutbound,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	dm.Plaintext = plaintext.String
	dm.DecryptedAt = decryptedAt.Time
	dm.SSBMsgSeq = ssbMsgSeq.Int64

	return &dm, nil
}

func (db *DB) ListDMsForFeed(ctx context.Context, feed string, limit int) ([]DirectMessage, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, ssb_msg_key, ssb_msg_seq, sender_feed, recipient_feed,
			   encrypted_content, plaintext, decrypted_at, created_at, received_at, is_outbound
		FROM direct_messages
		WHERE sender_feed = ? OR recipient_feed = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, feed, feed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dms []DirectMessage
	for rows.Next() {
		var dm DirectMessage
		var plaintext sql.NullString
		var decryptedAt sql.NullTime
		var ssbMsgSeq sql.NullInt64

		if err := rows.Scan(
			&dm.ID,
			&dm.SSBMsgKey,
			&ssbMsgSeq,
			&dm.SenderFeed,
			&dm.RecipientFeed,
			&dm.EncryptedContent,
			&plaintext,
			&decryptedAt,
			&dm.CreatedAt,
			&dm.ReceivedAt,
			&dm.IsOutbound,
		); err != nil {
			return nil, err
		}

		dm.Plaintext = plaintext.String
		dm.DecryptedAt = decryptedAt.Time
		dm.SSBMsgSeq = ssbMsgSeq.Int64

		dms = append(dms, dm)
	}

	return dms, rows.Err()
}

func (db *DB) ListDMConversations(ctx context.Context, feed string) ([]string, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT DISTINCT 
			CASE 
				WHEN sender_feed = ? THEN recipient_feed 
				ELSE sender_feed 
			END as other_party
		FROM direct_messages
		WHERE sender_feed = ? OR recipient_feed = ?
		ORDER BY MAX(created_at) DESC
	`, feed, feed, feed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []string
	for rows.Next() {
		var otherParty string
		if err := rows.Scan(&otherParty); err != nil {
			return nil, err
		}
		conversations = append(conversations, otherParty)
	}

	return conversations, rows.Err()
}

func (db *DB) UpdateDMDecrypted(ctx context.Context, msgKey, plaintext string) error {
	_, err := db.conn.ExecContext(ctx, `
		UPDATE direct_messages 
		SET plaintext = ?, decrypted_at = ?
		WHERE ssb_msg_key = ?
	`, plaintext, time.Now(), msgKey)
	return err
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}
