// Package db provides SQLite-backed persistence for bridge state and mappings.
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

func scanBridgedAccount(rows *sql.Rows) (BridgedAccount, error) {
	var acc BridgedAccount
	err := rows.Scan(&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active)
	return acc, err
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
	if err := db.ensureColumn("messages", "message_state", "TEXT NOT NULL DEFAULT 'pending'"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "defer_reason", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "defer_attempts", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "last_defer_attempt_at", "DATETIME"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "deleted_at", "DATETIME"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "deleted_seq", "INTEGER"); err != nil {
		return err
	}
	if err := db.ensureColumn("messages", "deleted_reason", "TEXT"); err != nil {
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

// AddBridgedAccount inserts or updates a bridged account row.
func (db *DB) AddBridgedAccount(ctx context.Context, acc BridgedAccount) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO bridged_accounts (at_did, ssb_feed_id, active) VALUES (?, ?, ?)
		 ON CONFLICT(at_did) DO UPDATE SET ssb_feed_id=excluded.ssb_feed_id, active=excluded.active`,
		acc.ATDID, acc.SSBFeedID, acc.Active,
	)
	if err != nil {
		return fmt.Errorf("add bridged account %s: %w", acc.ATDID, err)
	}
	return nil
}

// GetBridgedAccount returns the account row for atDID, or nil when absent.
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
		return nil, fmt.Errorf("get bridged account %s: %w", atDID, err)
	}
	return &acc, nil
}

// GetAllBridgedAccounts returns all bridged accounts sorted by newest first.
func (db *DB) GetAllBridgedAccounts(ctx context.Context) ([]BridgedAccount, error) {
	return querySlice(ctx, db.conn,
		"list bridged accounts",
		`SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts ORDER BY created_at DESC`,
		nil, scanBridgedAccount,
	)
}

// ListActiveBridgedAccounts returns active bridged accounts sorted by newest first.
func (db *DB) ListActiveBridgedAccounts(ctx context.Context) ([]BridgedAccount, error) {
	return querySlice(ctx, db.conn,
		"list active bridged accounts",
		`SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts WHERE active = 1 ORDER BY created_at DESC`,
		nil, scanBridgedAccount,
	)
}

// CountBridgedAccounts returns the total number of bridged accounts.
func (db *DB) CountBridgedAccounts(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridged_accounts`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count bridged accounts: %w", err)
	}
	return count, nil
}

// CountActiveBridgedAccounts returns the number of active bridged accounts.
func (db *DB) CountActiveBridgedAccounts(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridged_accounts WHERE active = 1`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active bridged accounts: %w", err)
	}
	return count, nil
}

const bridgedAccountStatsQuery = `
SELECT
  ba.at_did,
  ba.ssb_feed_id,
  ba.created_at,
  ba.active,
  COALESCE(s.total_messages, 0),
  COALESCE(s.published_messages, 0),
  COALESCE(s.failed_messages, 0),
  COALESCE(s.deferred_messages, 0),
  s.last_published_at
FROM bridged_accounts ba
LEFT JOIN (
  SELECT
    at_did,
    COUNT(*)                                                       AS total_messages,
    SUM(CASE WHEN message_state = 'published' THEN 1 ELSE 0 END)  AS published_messages,
    SUM(CASE WHEN message_state = 'failed' THEN 1 ELSE 0 END)     AS failed_messages,
    SUM(CASE WHEN message_state = 'deferred' THEN 1 ELSE 0 END)   AS deferred_messages,
    MAX(published_at)                                              AS last_published_at
  FROM messages
  GROUP BY at_did
) s ON s.at_did = ba.at_did
`

// ListActiveBridgedAccountsWithStats returns active accounts with per-bot message statistics.
func (db *DB) ListActiveBridgedAccountsWithStats(ctx context.Context) ([]BridgedAccountStats, error) {
	return querySlice(ctx, db.conn,
		"list active bridged accounts with stats",
		bridgedAccountStatsQuery+`WHERE ba.active = 1 ORDER BY ba.created_at DESC`,
		nil, scanBridgedAccountStatsRow,
	)
}

// ListBridgedAccountsWithStats returns all bridged accounts with per-bot message statistics.
func (db *DB) ListBridgedAccountsWithStats(ctx context.Context) ([]BridgedAccountStats, error) {
	return querySlice(ctx, db.conn,
		"list bridged accounts with stats",
		bridgedAccountStatsQuery+`ORDER BY ba.created_at DESC`,
		nil, scanBridgedAccountStatsRow,
	)
}

// ListActiveBridgedAccountsWithStatsSorted returns active accounts filtered/searched for room directory pages.
func (db *DB) ListActiveBridgedAccountsWithStatsSorted(ctx context.Context, search, sort string) ([]BridgedAccountStats, error) {
	search = strings.TrimSpace(search)
	sort = normalizeBotDirectorySort(sort)

	var query strings.Builder
	query.WriteString(bridgedAccountStatsQuery)
	query.WriteString(`WHERE ba.active = 1`)

	args := make([]interface{}, 0, 2)
	if search != "" {
		searchLike := "%" + search + "%"
		query.WriteString(` AND (ba.at_did LIKE ? OR ba.ssb_feed_id LIKE ?)`)
		args = append(args, searchLike, searchLike)
	}

	query.WriteString(` ORDER BY `)
	query.WriteString(botDirectoryOrderClause(sort))

	return querySlice(ctx, db.conn,
		"list active bridged accounts with stats sorted",
		query.String(),
		args,
		scanBridgedAccountStatsRow,
	)
}

// GetActiveBridgedAccountWithStats returns a single active account with stats, or nil.
func (db *DB) GetActiveBridgedAccountWithStats(ctx context.Context, atDID string) (*BridgedAccountStats, error) {
	row := db.conn.QueryRowContext(ctx, bridgedAccountStatsQuery+`WHERE ba.active = 1 AND ba.at_did = ?`, atDID)
	var acc BridgedAccountStats
	var lastPublishedAt sql.NullString
	err := row.Scan(
		&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active,
		&acc.TotalMessages, &acc.PublishedMessages, &acc.FailedMessages, &acc.DeferredMessages,
		&lastPublishedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active bridged account with stats %s: %w", atDID, err)
	}
	acc.LastPublishedAt = parseNullableTime(lastPublishedAt)
	return &acc, nil
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanBridgedAccountStats(row scannable) (BridgedAccountStats, error) {
	var acc BridgedAccountStats
	var lastPublishedAt sql.NullString
	err := row.Scan(
		&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active,
		&acc.TotalMessages, &acc.PublishedMessages, &acc.FailedMessages, &acc.DeferredMessages,
		&lastPublishedAt,
	)
	if err != nil {
		return acc, err
	}
	acc.LastPublishedAt = parseNullableTime(lastPublishedAt)
	return acc, nil
}

func scanBridgedAccountStatsRow(rows *sql.Rows) (BridgedAccountStats, error) {
	return scanBridgedAccountStats(rows)
}

func parseNullableTime(ns sql.NullString) *time.Time {
	if !ns.Valid || strings.TrimSpace(ns.String) == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, strings.TrimSpace(ns.String)); err == nil {
			return &t
		}
	}
	return nil
}

// AddMessage inserts or updates a message row keyed by AT URI.
func (db *DB) AddMessage(ctx context.Context, msg Message) error {
	if strings.TrimSpace(msg.MessageState) == "" {
		msg.MessageState = MessageStatePending
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().Truncate(time.Millisecond).UTC()
	} else {
		msg.CreatedAt = msg.CreatedAt.Truncate(time.Millisecond).UTC()
	}

	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO messages (
			at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(at_uri) DO UPDATE SET
		 	at_cid=excluded.at_cid,
		 	ssb_msg_ref=excluded.ssb_msg_ref,
		 	at_did=excluded.at_did,
		 	type=excluded.type,
		 	message_state=excluded.message_state,
		 	raw_at_json=excluded.raw_at_json,
		 	raw_ssb_json=excluded.raw_ssb_json,
		 	published_at=excluded.published_at,
		 	publish_error=excluded.publish_error,
		 	publish_attempts=messages.publish_attempts + excluded.publish_attempts,
		 	last_publish_attempt_at=excluded.last_publish_attempt_at,
		 	defer_reason=excluded.defer_reason,
		 	defer_attempts=messages.defer_attempts + excluded.defer_attempts,
		 	last_defer_attempt_at=excluded.last_defer_attempt_at,
		 	deleted_at=excluded.deleted_at,
		 	deleted_seq=excluded.deleted_seq,
		 	deleted_reason=excluded.deleted_reason`,
		msg.ATURI,
		msg.ATCID,
		msg.SSBMsgRef,
		msg.ATDID,
		msg.Type,
		msg.MessageState,
		msg.RawATJson,
		msg.RawSSBJson,
		msg.PublishedAt,
		msg.PublishError,
		msg.PublishAttempts,
		msg.LastPublishAttemptAt,
		msg.DeferReason,
		msg.DeferAttempts,
		msg.LastDeferAttemptAt,
		msg.DeletedAt,
		msg.DeletedSeq,
		msg.DeletedReason,
		msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("add message %s: %w", msg.ATURI, err)
	}
	return nil
}

// GetMessage returns the message row for atURI, or nil when absent.
func (db *DB) GetMessage(ctx context.Context, atURI string) (*Message, error) {
	var msg Message
	var ssbMsgRef, messageState, rawATJson, rawSSBJson, publishError, deferReason, deletedReason sql.NullString
	var publishedAt, lastPublishAttemptAt, lastDeferAttemptAt, deletedAt sql.NullTime
	var deletedSeq sql.NullInt64
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 WHERE at_uri = ?`,
		atURI,
	).Scan(
		&msg.ATURI,
		&msg.ATCID,
		&ssbMsgRef,
		&msg.ATDID,
		&msg.Type,
		&messageState,
		&rawATJson,
		&rawSSBJson,
		&publishedAt,
		&publishError,
		&msg.PublishAttempts,
		&lastPublishAttemptAt,
		&deferReason,
		&msg.DeferAttempts,
		&lastDeferAttemptAt,
		&deletedAt,
		&deletedSeq,
		&deletedReason,
		&msg.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get message %s: %w", atURI, err)
	}
	msg.CreatedAt = msg.CreatedAt.UTC()
	msg.SSBMsgRef = ssbMsgRef.String
	msg.MessageState = messageState.String
	msg.RawATJson = rawATJson.String
	msg.RawSSBJson = rawSSBJson.String
	msg.PublishError = publishError.String
	msg.DeferReason = deferReason.String
	msg.DeletedReason = deletedReason.String
	if publishedAt.Valid {
		t := publishedAt.Time
		msg.PublishedAt = &t
	}
	if lastPublishAttemptAt.Valid {
		t := lastPublishAttemptAt.Time
		msg.LastPublishAttemptAt = &t
	}
	if lastDeferAttemptAt.Valid {
		t := lastDeferAttemptAt.Time
		msg.LastDeferAttemptAt = &t
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		msg.DeletedAt = &t
	}
	if deletedSeq.Valid {
		seq := deletedSeq.Int64
		msg.DeletedSeq = &seq
	}
	return &msg, nil
}

// CountMessages returns the total number of stored messages.
func (db *DB) CountMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}
	return count, nil
}

// KnownPeer stores information about an SSB peer for gossip.
type KnownPeer struct {
	Addr      string
	PubKey    []byte
	LastSeen  *time.Time
	CreatedAt time.Time
}

// AddKnownPeer inserts or updates a known peer.
func (db *DB) AddKnownPeer(ctx context.Context, p KnownPeer) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO known_peers (addr, pubkey, last_seen)
		 VALUES (?, ?, ?)
		 ON CONFLICT(addr) DO UPDATE SET pubkey=excluded.pubkey, last_seen=excluded.last_seen`,
		p.Addr, p.PubKey, p.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("add known peer %s: %w", p.Addr, err)
	}
	return nil
}

// AddKnownPeerAddr is a helper for gossip.Database interface.
func (db *DB) AddKnownPeerAddr(ctx context.Context, addr string, pubKey []byte) error {
	return db.AddKnownPeer(ctx, KnownPeer{
		Addr:   addr,
		PubKey: pubKey,
	})
}

// GetKnownPeersBasic returns simple info for gossip.Database interface.
// Since we can't import gossip.PeerInfo here due to module boundaries,
// we might need a middle layer or just keep it as is.

// GetKnownPeers returns all known peers.
func (db *DB) GetKnownPeers(ctx context.Context) ([]KnownPeer, error) {
	return querySlice(ctx, db.conn,
		"list known peers",
		`SELECT addr, pubkey, last_seen, created_at FROM known_peers ORDER BY created_at DESC`,
		nil, scanKnownPeerRow,
	)
}

func scanKnownPeerRow(rows *sql.Rows) (KnownPeer, error) {
	var p KnownPeer
	var lastSeen sql.NullTime
	err := rows.Scan(&p.Addr, &p.PubKey, &lastSeen, &p.CreatedAt)
	if lastSeen.Valid {
		p.LastSeen = &lastSeen.Time
	}
	return p, err
}

// CountMessagesByDID returns the total number of stored messages for a specific author.
func (db *DB) CountMessagesByDID(ctx context.Context, atDID string) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE at_did = ?`, atDID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count messages for did %s: %w", atDID, err)
	}
	return count, nil
}

// GetRecentMessages returns the newest messages up to limit.
func (db *DB) GetRecentMessages(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 ORDER BY created_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent messages: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan recent messages: %w", err)
	}
	return messages, nil
}

// ListRecentPublishedMessagesByDID returns the newest published messages for a
// specific bridged DID.
func (db *DB) ListRecentPublishedMessagesByDID(ctx context.Context, atDID string, limit int) ([]Message, error) {
	atDID = strings.TrimSpace(atDID)
	if atDID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := db.conn.QueryContext(
		ctx,
		messageSelectColumns+`
		 FROM messages
		 WHERE at_did = ?
		   AND message_state = ?
		   AND TRIM(COALESCE(ssb_msg_ref, '')) <> ''
		 ORDER BY COALESCE(published_at, created_at) DESC, created_at DESC, at_uri DESC
		 LIMIT ?`,
		atDID,
		MessageStatePublished,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent published messages for did %s: %w", atDID, err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan recent published messages for did %s: %w", atDID, err)
	}
	return messages, nil
}

// ResetMessageForRetry resets a message state to pending and clears errors.
func (db *DB) ResetMessageForRetry(ctx context.Context, atURI string) error {
	_, err := db.conn.ExecContext(
		ctx,
		`UPDATE messages
		 SET message_state = ?,
		     publish_error = '',
		     publish_attempts = 0,
		     last_publish_attempt_at = NULL,
		     defer_reason = '',
		     defer_attempts = 0,
		     last_defer_attempt_at = NULL
		 WHERE at_uri = ?`,
		MessageStatePending,
		atURI,
	)
	if err != nil {
		return fmt.Errorf("reset message %s: %w", atURI, err)
	}
	return nil
}

// ListPublishedMessagesGlobal returns the newest published messages across all bots.
func (db *DB) ListPublishedMessagesGlobal(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.QueryContext(
		ctx,
		messageSelectColumns+`
		 FROM messages
		 WHERE message_state = ?
		   AND TRIM(COALESCE(ssb_msg_ref, '')) <> ''
		 ORDER BY COALESCE(published_at, created_at) DESC, created_at DESC, at_uri DESC
		 LIMIT ?`,
		MessageStatePublished,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query global published messages: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan global published messages: %w", err)
	}
	return messages, nil
}

const messageSelectColumns = `SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at`

// ListMessages returns messages filtered and sorted for interactive UI browsing.
func (db *DB) ListMessages(ctx context.Context, query MessageListQuery) ([]Message, error) {
	query = normalizeMessageListQuery(query)

	var builder strings.Builder
	builder.WriteString(messageSelectColumns)
	builder.WriteString(` FROM messages WHERE 1=1`)

	args := make([]interface{}, 0, 12)
	appendMessageListFilters(&builder, &args, query)

	builder.WriteString(` ORDER BY `)
	builder.WriteString(messageOrderClause(query.Sort))
	builder.WriteString(` LIMIT ?`)
	args = append(args, query.Limit)

	rows, err := db.conn.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan messages: %w", err)
	}
	return messages, nil
}

// ListMessagesPage returns one keyset-paginated page of filtered messages.
func (db *DB) ListMessagesPage(ctx context.Context, query MessageListQuery) (MessagePage, error) {
	query = normalizeMessageListQuery(query)
	page := MessagePage{}

	// Keep compatibility for older non-keyset sorts while using keyset for newest/oldest.
	if !supportsMessageKeysetSort(query.Sort) {
		legacyRows, err := db.ListMessages(ctx, MessageListQuery{
			Search:   query.Search,
			Type:     query.Type,
			State:    query.State,
			Sort:     query.Sort,
			Limit:    query.Limit + 1,
			ATDID:    query.ATDID,
			HasIssue: query.HasIssue,
		})
		if err != nil {
			return page, err
		}
		if len(legacyRows) > query.Limit {
			page.HasNext = true
			legacyRows = legacyRows[:query.Limit]
		}
		page.Messages = legacyRows
		if page.HasNext && len(legacyRows) > 0 {
			page.NextCursor = encodeMessageListCursor(messageListCursor{
				CreatedAt: legacyRows[len(legacyRows)-1].CreatedAt,
				ATURI:     legacyRows[len(legacyRows)-1].ATURI,
			})
		}
		return page, nil
	}

	var cursor messageListCursor
	cursorProvided := strings.TrimSpace(query.Cursor) != ""
	if cursorProvided {
		decoded, ok := decodeMessageListCursor(query.Cursor)
		if !ok {
			cursorProvided = false
		} else {
			cursor = decoded
		}
	}

	reverseQuery := false
	var builder strings.Builder
	builder.WriteString(messageSelectColumns)
	builder.WriteString(` FROM messages WHERE 1=1`)
	args := make([]interface{}, 0, 16)
	appendMessageListFilters(&builder, &args, query)

	if cursorProvided {
		clause, clauseArgs, reverse := messageKeysetClause(query.Sort, query.Direction, cursor)
		if clause != "" {
			builder.WriteString(` AND `)
			builder.WriteString(clause)
			args = append(args, clauseArgs...)
			reverseQuery = reverse
		}
	}

	builder.WriteString(` ORDER BY `)
	builder.WriteString(messageKeysetOrder(query.Sort, reverseQuery))
	builder.WriteString(` LIMIT ?`)
	args = append(args, query.Limit+1)

	rows, err := db.conn.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return page, err
	}

	hasMore := len(messages) > query.Limit
	if hasMore {
		messages = messages[:query.Limit]
	}
	if reverseQuery {
		reverseMessages(messages)
	}

	page.Messages = messages
	if query.Direction == "prev" {
		page.HasPrev = hasMore
		page.HasNext = cursorProvided
	} else {
		page.HasPrev = cursorProvided
		page.HasNext = hasMore
	}

	if len(messages) > 0 {
		first := messageListCursor{CreatedAt: messages[0].CreatedAt, ATURI: messages[0].ATURI}
		last := messageListCursor{CreatedAt: messages[len(messages)-1].CreatedAt, ATURI: messages[len(messages)-1].ATURI}
		if page.HasPrev {
			page.PrevCursor = encodeMessageListCursor(first)
		}
		if page.HasNext {
			page.NextCursor = encodeMessageListCursor(last)
		}
	}

	return page, nil
}

// ListMessageTypes returns the distinct record types currently stored.
func (db *DB) ListMessageTypes(ctx context.Context) ([]string, error) {
	return querySlice(ctx, db.conn,
		"list message types",
		`SELECT DISTINCT type
		 FROM messages
		 WHERE TRIM(COALESCE(type, '')) <> ''
		 ORDER BY type ASC`,
		nil,
		scanMessageTypeRow,
	)
}

// CountPublishedMessages returns the number of messages with an SSB message ref.
func (db *DB) CountPublishedMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> ''`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count published messages: %w", err)
	}
	return count, nil
}

// CountPublishFailures returns the number of messages with a publish error.
func (db *DB) CountPublishFailures(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE message_state = ?`, MessageStateFailed).Scan(&count); err != nil {
		return 0, fmt.Errorf("count publish failures: %w", err)
	}
	return count, nil
}

func (db *DB) CountDeferredMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE message_state = ?`, MessageStateDeferred).Scan(&count); err != nil {
		return 0, fmt.Errorf("count deferred messages: %w", err)
	}
	return count, nil
}

func (db *DB) CountDeletedMessages(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE message_state = ?`, MessageStateDeleted).Scan(&count); err != nil {
		return 0, fmt.Errorf("count deleted messages: %w", err)
	}
	return count, nil
}

// ListTopDeferredReasons returns the most common deferred reasons.
func (db *DB) ListTopDeferredReasons(ctx context.Context, limit int) ([]DeferredReasonCount, error) {
	if limit <= 0 {
		limit = 5
	}

	return querySlice(ctx, db.conn,
		"list top deferred reasons",
		`SELECT defer_reason, COUNT(*) AS reason_count
		 FROM messages
		 WHERE message_state = ?
		   AND TRIM(COALESCE(defer_reason, '')) <> ''
		 GROUP BY defer_reason
		 ORDER BY reason_count DESC, defer_reason ASC
		 LIMIT ?`,
		[]any{
			MessageStateDeferred,
			limit,
		},
		scanDeferredReasonCountRow,
	)
}

// ListTopIssueAccounts returns bridged accounts ranked by issue volume.
func (db *DB) ListTopIssueAccounts(ctx context.Context, limit int) ([]AccountIssueSummary, error) {
	if limit <= 0 {
		limit = 5
	}

	return querySlice(ctx, db.conn,
		"list top issue accounts",
		`SELECT
		   ba.at_did,
		   ba.ssb_feed_id,
		   ba.active,
		   COALESCE(m.total_messages, 0) AS total_messages,
		   COALESCE(m.issue_messages, 0) AS issue_messages,
		   COALESCE(m.failed_messages, 0) AS failed_messages,
		   COALESCE(m.deferred_messages, 0) AS deferred_messages,
		   COALESCE(m.deleted_messages, 0) AS deleted_messages
		 FROM bridged_accounts ba
		 LEFT JOIN (
		   SELECT
		     at_did,
		     COUNT(*) AS total_messages,
		     SUM(CASE WHEN message_state IN ('failed', 'deferred', 'deleted') THEN 1 ELSE 0 END) AS issue_messages,
		     SUM(CASE WHEN message_state = 'failed' THEN 1 ELSE 0 END) AS failed_messages,
		     SUM(CASE WHEN message_state = 'deferred' THEN 1 ELSE 0 END) AS deferred_messages,
		     SUM(CASE WHEN message_state = 'deleted' THEN 1 ELSE 0 END) AS deleted_messages
		   FROM messages
		   GROUP BY at_did
		 ) m ON m.at_did = ba.at_did
		 WHERE COALESCE(m.issue_messages, 0) > 0
		 ORDER BY issue_messages DESC, total_messages DESC, ba.at_did ASC
		 LIMIT ?`,
		[]any{limit},
		scanAccountIssueSummaryRow,
	)
}

// GetPublishFailures returns failed message rows up to limit.
func (db *DB) GetPublishFailures(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 WHERE message_state IN (?, ?)
		 ORDER BY COALESCE(last_publish_attempt_at, last_defer_attempt_at, created_at) DESC
		 LIMIT ?`,
		MessageStateFailed,
		MessageStateDeferred,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query publish failures: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan publish failures: %w", err)
	}
	return messages, nil
}

// GetRetryCandidates returns failed unpublished messages eligible for retry.
func (db *DB) GetRetryCandidates(ctx context.Context, limit int, atDID string, maxAttempts int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}

	var query strings.Builder
	query.WriteString(
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, message_state, raw_at_json, raw_ssb_json, published_at, publish_error, publish_attempts, last_publish_attempt_at, defer_reason, defer_attempts, last_defer_attempt_at, deleted_at, deleted_seq, deleted_reason, created_at
		 FROM messages
		 WHERE message_state = ?
		   AND (ssb_msg_ref IS NULL OR ssb_msg_ref = '')
		   AND publish_attempts < ?`,
	)

	args := []interface{}{MessageStateFailed, maxAttempts}
	if strings.TrimSpace(atDID) != "" {
		query.WriteString(" AND at_did = ?")
		args = append(args, strings.TrimSpace(atDID))
	}
	query.WriteString(" ORDER BY COALESCE(last_publish_attempt_at, created_at) ASC LIMIT ?")
	args = append(args, limit)

	rows, err := db.conn.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query retry candidates: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan retry candidates: %w", err)
	}
	return messages, nil
}

func (db *DB) GetDeferredCandidates(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	// Topological ordering: records whose dependencies are already resolved
	// (published, failed, or absent) come before records whose dependencies
	// are themselves still deferred. This prevents round-robin starvation
	// in self-referencing reply chains where parent→child are both deferred.
	//
	// The subquery extracts the first at:// URI from defer_reason (which is
	// formatted as "_atproto_key=at://..." possibly with ";"-separated pairs)
	// and checks whether that URI belongs to a still-deferred record.
	rows, err := db.conn.QueryContext(
		ctx,
		`SELECT m.at_uri, m.at_cid, m.ssb_msg_ref, m.at_did, m.type, m.message_state, m.raw_at_json, m.raw_ssb_json, m.published_at, m.publish_error, m.publish_attempts, m.last_publish_attempt_at, m.defer_reason, m.defer_attempts, m.last_defer_attempt_at, m.deleted_at, m.deleted_seq, m.deleted_reason, m.created_at
		 FROM messages m
		 LEFT JOIN messages dep ON dep.at_uri = SUBSTR(m.defer_reason, INSTR(m.defer_reason, 'at://'), CASE WHEN INSTR(SUBSTR(m.defer_reason, INSTR(m.defer_reason, 'at://')), ';') > 0 THEN INSTR(SUBSTR(m.defer_reason, INSTR(m.defer_reason, 'at://')), ';') - 1 ELSE LENGTH(m.defer_reason) END) AND dep.message_state = ?
		 WHERE m.message_state = ?
		 ORDER BY CASE WHEN dep.at_uri IS NOT NULL THEN 1 ELSE 0 END ASC, COALESCE(m.last_defer_attempt_at, m.created_at) ASC
		 LIMIT ?`,
		MessageStateDeferred,
		MessageStateDeferred,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query deferred candidates: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessagesRows(rows)
	if err != nil {
		return nil, fmt.Errorf("scan deferred candidates: %w", err)
	}
	return messages, nil
}

func (db *DB) GetLatestDeferredReason(ctx context.Context) (string, bool, error) {
	var reason sql.NullString
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT defer_reason
		 FROM messages
		 WHERE message_state = ? AND defer_reason IS NOT NULL AND defer_reason <> ''
		 ORDER BY COALESCE(last_defer_attempt_at, created_at) DESC
		 LIMIT 1`,
		MessageStateDeferred,
	).Scan(&reason)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("get latest deferred reason: %w", err)
	}
	if !reason.Valid || strings.TrimSpace(reason.String) == "" {
		return "", false, nil
	}
	return reason.String, true, nil
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
		limit = 50
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

func normalizeBotDirectorySort(sort string) string {
	switch strings.TrimSpace(sort) {
	case "newest", "deferred_desc":
		return strings.TrimSpace(sort)
	default:
		return "activity_desc"
	}
}

func botDirectoryOrderClause(sort string) string {
	switch normalizeBotDirectorySort(sort) {
	case "newest":
		return "ba.created_at DESC"
	case "deferred_desc":
		return "COALESCE(s.deferred_messages, 0) DESC, COALESCE(s.failed_messages, 0) DESC, ba.created_at DESC"
	default:
		return "COALESCE(s.total_messages, 0) DESC, COALESCE(s.published_messages, 0) DESC, ba.created_at DESC"
	}
}

func scanMessageTypeRow(rows *sql.Rows) (string, error) {
	var recordType string
	if err := rows.Scan(&recordType); err != nil {
		return "", err
	}
	return recordType, nil
}

func scanDeferredReasonCountRow(rows *sql.Rows) (DeferredReasonCount, error) {
	var stat DeferredReasonCount
	if err := rows.Scan(&stat.Reason, &stat.Count); err != nil {
		return DeferredReasonCount{}, err
	}
	return stat, nil
}

func scanAccountIssueSummaryRow(rows *sql.Rows) (AccountIssueSummary, error) {
	var stat AccountIssueSummary
	var active bool
	if err := rows.Scan(
		&stat.ATDID,
		&stat.SSBFeedID,
		&active,
		&stat.TotalMessages,
		&stat.IssueMessages,
		&stat.FailedMessages,
		&stat.DeferredCount,
		&stat.DeletedCount,
	); err != nil {
		return AccountIssueSummary{}, err
	}
	stat.Active = active
	return stat, nil
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
