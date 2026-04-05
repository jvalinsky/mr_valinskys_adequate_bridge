package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

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

func scanBridgedAccount(rows *sql.Rows) (BridgedAccount, error) {
	var acc BridgedAccount
	err := rows.Scan(&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active)
	return acc, err
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

func (db *DB) GetAllBridgedAccounts(ctx context.Context) ([]BridgedAccount, error) {
	return querySlice(ctx, db.conn,
		"list bridged accounts",
		`SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts ORDER BY created_at DESC`,
		nil, scanBridgedAccount,
	)
}

func (db *DB) ListActiveBridgedAccounts(ctx context.Context) ([]BridgedAccount, error) {
	return querySlice(ctx, db.conn,
		"list active bridged accounts",
		`SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts WHERE active = 1 ORDER BY created_at DESC`,
		nil, scanBridgedAccount,
	)
}

func (db *DB) CountBridgedAccounts(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridged_accounts`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count bridged accounts: %w", err)
	}
	return count, nil
}

func (db *DB) CountActiveBridgedAccounts(ctx context.Context) (int, error) {
	var count int
	if err := db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM bridged_accounts WHERE active = 1`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active bridged accounts: %w", err)
	}
	return count, nil
}

func (db *DB) ListActiveBridgedAccountsWithStats(ctx context.Context) ([]BridgedAccountStats, error) {
	return querySlice(ctx, db.conn,
		"list active bridged accounts with stats",
		bridgedAccountStatsQuery+`WHERE ba.active = 1 ORDER BY ba.created_at DESC`,
		nil, scanBridgedAccountStatsRow,
	)
}

func (db *DB) ListBridgedAccountsWithStats(ctx context.Context) ([]BridgedAccountStats, error) {
	return querySlice(ctx, db.conn,
		"list bridged accounts with stats",
		bridgedAccountStatsQuery+`ORDER BY ba.created_at DESC`,
		nil, scanBridgedAccountStatsRow,
	)
}

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
