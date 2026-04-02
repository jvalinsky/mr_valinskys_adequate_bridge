package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type ATProtoSource struct {
	SourceKey   string
	RelayURL    string
	LastSeq     int64
	ConnectedAt *time.Time
	UpdatedAt   time.Time
}

type ATProtoRepo struct {
	DID              string
	Tracking         bool
	Reason           string
	SyncState        string
	Generation       int64
	CurrentRev       string
	CurrentCommitCID string
	CurrentDataCID   string
	LastFirehoseSeq  *int64
	LastBackfillAt   *time.Time
	LastEventCursor  *int64
	Handle           string
	PDSURL           string
	AccountActive    *bool
	AccountStatus    string
	LastIdentityAt   *time.Time
	LastAccountAt    *time.Time
	LastError        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ATProtoCommitBufferItem struct {
	ID           int64
	DID          string
	Generation   int64
	Rev          string
	Seq          int64
	RawEventJSON string
	CreatedAt    time.Time
}

type ATProtoRecord struct {
	DID        string
	Collection string
	RKey       string
	ATURI      string
	ATCID      string
	RecordJSON string
	LastRev    string
	LastSeq    *int64
	Deleted    bool
	DeletedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ATProtoRecordEvent struct {
	Cursor     int64
	DID        string
	Collection string
	RKey       string
	ATURI      string
	ATCID      string
	Action     string
	Live       bool
	Rev        string
	Seq        *int64
	RecordJSON string
	CreatedAt  time.Time
}

func (db *DB) UpsertATProtoSource(ctx context.Context, source ATProtoSource) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO atproto_sources (source_key, relay_url, last_seq, connected_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_key) DO UPDATE SET
			relay_url=excluded.relay_url,
			last_seq=excluded.last_seq,
			connected_at=excluded.connected_at,
			updated_at=CURRENT_TIMESTAMP
	`, source.SourceKey, source.RelayURL, source.LastSeq, source.ConnectedAt)
	if err != nil {
		return fmt.Errorf("upsert atproto source %s: %w", source.SourceKey, err)
	}
	return nil
}

func (db *DB) GetATProtoSource(ctx context.Context, sourceKey string) (*ATProtoSource, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT source_key, relay_url, last_seq, connected_at, updated_at
		FROM atproto_sources
		WHERE source_key = ?
	`, sourceKey)

	var source ATProtoSource
	var connectedAt sql.NullTime
	if err := row.Scan(&source.SourceKey, &source.RelayURL, &source.LastSeq, &connectedAt, &source.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get atproto source %s: %w", sourceKey, err)
	}
	source.ConnectedAt = nullTimePtr(connectedAt)
	return &source, nil
}

func (db *DB) UpsertATProtoRepo(ctx context.Context, repo ATProtoRepo) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO atproto_repos (
			did, tracking, reason, sync_state, generation, current_rev, current_commit_cid, current_data_cid,
			last_firehose_seq, last_backfill_at, last_event_cursor, handle, pds_url, account_active,
			account_status, last_identity_at, last_account_at, last_error, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(did) DO UPDATE SET
			tracking=excluded.tracking,
			reason=excluded.reason,
			sync_state=excluded.sync_state,
			generation=excluded.generation,
			current_rev=excluded.current_rev,
			current_commit_cid=excluded.current_commit_cid,
			current_data_cid=excluded.current_data_cid,
			last_firehose_seq=excluded.last_firehose_seq,
			last_backfill_at=excluded.last_backfill_at,
			last_event_cursor=excluded.last_event_cursor,
			handle=excluded.handle,
			pds_url=excluded.pds_url,
			account_active=excluded.account_active,
			account_status=excluded.account_status,
			last_identity_at=excluded.last_identity_at,
			last_account_at=excluded.last_account_at,
			last_error=excluded.last_error,
			updated_at=CURRENT_TIMESTAMP
	`,
		repo.DID,
		repo.Tracking,
		repo.Reason,
		repo.SyncState,
		repo.Generation,
		repo.CurrentRev,
		repo.CurrentCommitCID,
		repo.CurrentDataCID,
		repo.LastFirehoseSeq,
		repo.LastBackfillAt,
		repo.LastEventCursor,
		repo.Handle,
		repo.PDSURL,
		repo.AccountActive,
		repo.AccountStatus,
		repo.LastIdentityAt,
		repo.LastAccountAt,
		repo.LastError,
	)
	if err != nil {
		return fmt.Errorf("upsert atproto repo %s: %w", repo.DID, err)
	}
	return nil
}

func (db *DB) GetATProtoRepo(ctx context.Context, did string) (*ATProtoRepo, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT did, tracking, reason, sync_state, generation, current_rev, current_commit_cid, current_data_cid,
		       last_firehose_seq, last_backfill_at, last_event_cursor, handle, pds_url, account_active,
		       account_status, last_identity_at, last_account_at, last_error, created_at, updated_at
		FROM atproto_repos
		WHERE did = ?
	`, did)

	var repo ATProtoRepo
	var lastFirehoseSeq sql.NullInt64
	var lastBackfillAt sql.NullTime
	var lastEventCursor sql.NullInt64
	var accountActive sql.NullBool
	var lastIdentityAt sql.NullTime
	var lastAccountAt sql.NullTime
	if err := row.Scan(
		&repo.DID,
		&repo.Tracking,
		&repo.Reason,
		&repo.SyncState,
		&repo.Generation,
		&repo.CurrentRev,
		&repo.CurrentCommitCID,
		&repo.CurrentDataCID,
		&lastFirehoseSeq,
		&lastBackfillAt,
		&lastEventCursor,
		&repo.Handle,
		&repo.PDSURL,
		&accountActive,
		&repo.AccountStatus,
		&lastIdentityAt,
		&lastAccountAt,
		&repo.LastError,
		&repo.CreatedAt,
		&repo.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get atproto repo %s: %w", did, err)
	}
	repo.LastFirehoseSeq = nullInt64Ptr(lastFirehoseSeq)
	repo.LastBackfillAt = nullTimePtr(lastBackfillAt)
	repo.LastEventCursor = nullInt64Ptr(lastEventCursor)
	repo.AccountActive = nullBoolPtr(accountActive)
	repo.LastIdentityAt = nullTimePtr(lastIdentityAt)
	repo.LastAccountAt = nullTimePtr(lastAccountAt)
	return &repo, nil
}

func (db *DB) ListTrackedATProtoRepos(ctx context.Context, state string) ([]ATProtoRepo, error) {
	query := `
		SELECT did, tracking, reason, sync_state, generation, current_rev, current_commit_cid, current_data_cid,
		       last_firehose_seq, last_backfill_at, last_event_cursor, handle, pds_url, account_active,
		       account_status, last_identity_at, last_account_at, last_error, created_at, updated_at
		FROM atproto_repos
		WHERE tracking = 1
	`
	args := []any{}
	if state != "" {
		query += ` AND sync_state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY did`

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list atproto repos: %w", err)
	}
	defer rows.Close()

	var repos []ATProtoRepo
	for rows.Next() {
		var repo ATProtoRepo
		var lastFirehoseSeq sql.NullInt64
		var lastBackfillAt sql.NullTime
		var lastEventCursor sql.NullInt64
		var accountActive sql.NullBool
		var lastIdentityAt sql.NullTime
		var lastAccountAt sql.NullTime
		if err := rows.Scan(
			&repo.DID,
			&repo.Tracking,
			&repo.Reason,
			&repo.SyncState,
			&repo.Generation,
			&repo.CurrentRev,
			&repo.CurrentCommitCID,
			&repo.CurrentDataCID,
			&lastFirehoseSeq,
			&lastBackfillAt,
			&lastEventCursor,
			&repo.Handle,
			&repo.PDSURL,
			&accountActive,
			&repo.AccountStatus,
			&lastIdentityAt,
			&lastAccountAt,
			&repo.LastError,
			&repo.CreatedAt,
			&repo.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan atproto repo: %w", err)
		}
		repo.LastFirehoseSeq = nullInt64Ptr(lastFirehoseSeq)
		repo.LastBackfillAt = nullTimePtr(lastBackfillAt)
		repo.LastEventCursor = nullInt64Ptr(lastEventCursor)
		repo.AccountActive = nullBoolPtr(accountActive)
		repo.LastIdentityAt = nullTimePtr(lastIdentityAt)
		repo.LastAccountAt = nullTimePtr(lastAccountAt)
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list atproto repos: %w", err)
	}
	return repos, nil
}

func (db *DB) AddATProtoCommitBufferItem(ctx context.Context, item ATProtoCommitBufferItem) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO atproto_commit_buffer (did, generation, rev, seq, raw_event_json)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(did, generation, rev) DO UPDATE SET
			seq=excluded.seq,
			raw_event_json=excluded.raw_event_json
	`, item.DID, item.Generation, item.Rev, item.Seq, item.RawEventJSON)
	if err != nil {
		return fmt.Errorf("insert atproto commit buffer %s/%s: %w", item.DID, item.Rev, err)
	}
	return nil
}

func (db *DB) ListATProtoCommitBufferItems(ctx context.Context, did string, generation int64) ([]ATProtoCommitBufferItem, error) {
	rows, err := db.conn.QueryContext(ctx, `
		SELECT id, did, generation, rev, seq, raw_event_json, created_at
		FROM atproto_commit_buffer
		WHERE did = ? AND generation = ?
		ORDER BY seq, rev
	`, did, generation)
	if err != nil {
		return nil, fmt.Errorf("list atproto commit buffer %s: %w", did, err)
	}
	defer rows.Close()

	var items []ATProtoCommitBufferItem
	for rows.Next() {
		var item ATProtoCommitBufferItem
		if err := rows.Scan(&item.ID, &item.DID, &item.Generation, &item.Rev, &item.Seq, &item.RawEventJSON, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan atproto commit buffer: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list atproto commit buffer %s: %w", did, err)
	}
	return items, nil
}

func (db *DB) DeleteATProtoCommitBufferItems(ctx context.Context, did string, generation int64) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM atproto_commit_buffer WHERE did = ? AND generation = ?`, did, generation)
	if err != nil {
		return fmt.Errorf("delete atproto commit buffer %s: %w", did, err)
	}
	return nil
}

func (db *DB) UpsertATProtoRecord(ctx context.Context, record ATProtoRecord) error {
	_, err := db.conn.ExecContext(ctx, `
		INSERT INTO atproto_records (did, collection, rkey, at_uri, at_cid, record_json, last_rev, last_seq, deleted, deleted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(did, collection, rkey) DO UPDATE SET
			at_uri=excluded.at_uri,
			at_cid=excluded.at_cid,
			record_json=excluded.record_json,
			last_rev=excluded.last_rev,
			last_seq=excluded.last_seq,
			deleted=excluded.deleted,
			deleted_at=excluded.deleted_at,
			updated_at=CURRENT_TIMESTAMP
	`,
		record.DID, record.Collection, record.RKey, record.ATURI, record.ATCID, record.RecordJSON,
		record.LastRev, record.LastSeq, record.Deleted, record.DeletedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert atproto record %s: %w", record.ATURI, err)
	}
	return nil
}

func (db *DB) GetATProtoRecord(ctx context.Context, atURI string) (*ATProtoRecord, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT did, collection, rkey, at_uri, at_cid, record_json, last_rev, last_seq, deleted, deleted_at, created_at, updated_at
		FROM atproto_records
		WHERE at_uri = ?
	`, atURI)

	var record ATProtoRecord
	var lastSeq sql.NullInt64
	var deletedAt sql.NullTime
	if err := row.Scan(
		&record.DID,
		&record.Collection,
		&record.RKey,
		&record.ATURI,
		&record.ATCID,
		&record.RecordJSON,
		&record.LastRev,
		&lastSeq,
		&record.Deleted,
		&deletedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get atproto record %s: %w", atURI, err)
	}
	record.LastSeq = nullInt64Ptr(lastSeq)
	record.DeletedAt = nullTimePtr(deletedAt)
	return &record, nil
}

func (db *DB) ListATProtoRecords(ctx context.Context, did, collection, cursor string, limit int) ([]ATProtoRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.conn.QueryContext(ctx, `
		SELECT did, collection, rkey, at_uri, at_cid, record_json, last_rev, last_seq, deleted, deleted_at, created_at, updated_at
		FROM atproto_records
		WHERE did = ? AND collection = ? AND at_uri > ?
		ORDER BY at_uri
		LIMIT ?
	`, did, collection, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list atproto records %s/%s: %w", did, collection, err)
	}
	defer rows.Close()

	var records []ATProtoRecord
	for rows.Next() {
		var record ATProtoRecord
		var lastSeq sql.NullInt64
		var deletedAt sql.NullTime
		if err := rows.Scan(
			&record.DID,
			&record.Collection,
			&record.RKey,
			&record.ATURI,
			&record.ATCID,
			&record.RecordJSON,
			&record.LastRev,
			&lastSeq,
			&record.Deleted,
			&deletedAt,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan atproto record: %w", err)
		}
		record.LastSeq = nullInt64Ptr(lastSeq)
		record.DeletedAt = nullTimePtr(deletedAt)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list atproto records %s/%s: %w", did, collection, err)
	}
	return records, nil
}

func (db *DB) AppendATProtoEvent(ctx context.Context, event ATProtoRecordEvent) (int64, error) {
	result, err := db.conn.ExecContext(ctx, `
		INSERT INTO atproto_event_log (did, collection, rkey, at_uri, at_cid, action, live, rev, seq, record_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.DID, event.Collection, event.RKey, event.ATURI, event.ATCID, event.Action, event.Live, event.Rev, event.Seq, event.RecordJSON)
	if err != nil {
		return 0, fmt.Errorf("append atproto event %s: %w", event.ATURI, err)
	}
	cursor, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("append atproto event last insert id: %w", err)
	}
	return cursor, nil
}

func (db *DB) ListATProtoEventsAfter(ctx context.Context, cursor int64, limit int) ([]ATProtoRecordEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.conn.QueryContext(ctx, `
		SELECT cursor, did, collection, rkey, at_uri, at_cid, action, live, rev, seq, record_json, created_at
		FROM atproto_event_log
		WHERE cursor > ?
		ORDER BY cursor
		LIMIT ?
	`, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list atproto events after %d: %w", cursor, err)
	}
	defer rows.Close()

	var events []ATProtoRecordEvent
	for rows.Next() {
		var event ATProtoRecordEvent
		var seq sql.NullInt64
		if err := rows.Scan(
			&event.Cursor,
			&event.DID,
			&event.Collection,
			&event.RKey,
			&event.ATURI,
			&event.ATCID,
			&event.Action,
			&event.Live,
			&event.Rev,
			&seq,
			&event.RecordJSON,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan atproto event: %w", err)
		}
		event.Seq = nullInt64Ptr(seq)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list atproto events after %d: %w", cursor, err)
	}
	return events, nil
}

func (db *DB) GetLatestATProtoEventCursor(ctx context.Context) (int64, bool, error) {
	row := db.conn.QueryRowContext(ctx, `
		SELECT cursor
		FROM atproto_event_log
		ORDER BY cursor DESC
		LIMIT 1
	`)

	var cursor int64
	if err := row.Scan(&cursor); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("get latest atproto event cursor: %w", err)
	}
	return cursor, true, nil
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}

func nullBoolPtr(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	v := value.Bool
	return &v
}
