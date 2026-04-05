-- name: UpsertATProtoSource :exec
INSERT INTO atproto_sources (source_key, relay_url, last_seq, connected_at, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(source_key) DO UPDATE SET
	relay_url=excluded.relay_url,
	last_seq=excluded.last_seq,
	connected_at=excluded.connected_at,
	updated_at=CURRENT_TIMESTAMP;

-- name: GetATProtoSource :one
SELECT source_key, relay_url, last_seq, connected_at, updated_at
FROM atproto_sources
WHERE source_key = ?;

-- name: UpsertATProtoRepo :exec
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
	updated_at=CURRENT_TIMESTAMP;

-- name: GetATProtoRepo :one
SELECT did, tracking, reason, sync_state, generation, current_rev, current_commit_cid, current_data_cid,
	   last_firehose_seq, last_backfill_at, last_event_cursor, handle, pds_url, account_active,
	   account_status, last_identity_at, last_account_at, last_error, created_at, updated_at
FROM atproto_repos
WHERE did = ?;

-- name: ListTrackedATProtoRepos :many
SELECT did, tracking, reason, sync_state, generation, current_rev, current_commit_cid, current_data_cid,
	   last_firehose_seq, last_backfill_at, last_event_cursor, handle, pds_url, account_active,
	   account_status, last_identity_at, last_account_at, last_error, created_at, updated_at
FROM atproto_repos
WHERE tracking = 1
ORDER BY did;

-- name: AddATProtoCommitBufferItem :exec
INSERT INTO atproto_commit_buffer (did, generation, rev, seq, raw_event_json)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(did, generation, rev) DO UPDATE SET
	seq=excluded.seq,
	raw_event_json=excluded.raw_event_json;

-- name: ListATProtoCommitBufferItems :many
SELECT id, did, generation, rev, seq, raw_event_json, created_at
FROM atproto_commit_buffer
WHERE did = ? AND generation = ?
ORDER BY seq, rev;

-- name: DeleteATProtoCommitBufferItems :exec
DELETE FROM atproto_commit_buffer WHERE did = ? AND generation = ?;

-- name: UpsertATProtoRecord :exec
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
	updated_at=CURRENT_TIMESTAMP;

-- name: GetATProtoRecord :one
SELECT did, collection, rkey, at_uri, at_cid, record_json, last_rev, last_seq, deleted, deleted_at, created_at, updated_at
FROM atproto_records
WHERE at_uri = ?;

-- name: ListATProtoRecords :many
SELECT did, collection, rkey, at_uri, at_cid, record_json, last_rev, last_seq, deleted, deleted_at, created_at, updated_at
FROM atproto_records
WHERE did = ? AND collection = ?
ORDER BY at_uri
LIMIT ?;

-- name: AppendATProtoEvent :exec
INSERT INTO atproto_event_log (did, collection, rkey, at_uri, at_cid, action, live, rev, seq, record_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListATProtoEventsAfter :many
SELECT cursor, did, collection, rkey, at_uri, at_cid, action, live, rev, seq, record_json, created_at
FROM atproto_event_log
WHERE cursor > ?
ORDER BY cursor
LIMIT ?;

-- name: GetLatestATProtoEventCursor :one
SELECT cursor
FROM atproto_event_log
ORDER BY cursor DESC
LIMIT 1;