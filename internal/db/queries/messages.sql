-- name: GetMessage :one
SELECT * FROM messages WHERE at_uri = :at_uri;

-- name: AddMessage :exec
INSERT INTO messages (
    at_uri, at_cid, ssb_msg_ref, at_did, type, message_state,
    raw_at_json, raw_ssb_json, published_at, publish_error,
    publish_attempts, last_publish_attempt_at, defer_reason,
    defer_attempts, last_defer_attempt_at, deleted_at,
    deleted_seq, deleted_reason, created_at
) VALUES (
    :at_uri, :at_cid, :ssb_msg_ref, :at_did, :type, :message_state,
    :raw_at_json, :raw_ssb_json, :published_at, :publish_error,
    :publish_attempts, :last_publish_attempt_at, :defer_reason,
    :defer_attempts, :last_defer_attempt_at, :deleted_at,
    :deleted_seq, :deleted_reason, :created_at
) ON CONFLICT(at_uri) DO UPDATE SET
    at_cid = excluded.at_cid,
    ssb_msg_ref = excluded.ssb_msg_ref,
    at_did = excluded.at_did,
    type = excluded.type,
    message_state = excluded.message_state,
    raw_at_json = excluded.raw_at_json,
    raw_ssb_json = excluded.raw_ssb_json,
    published_at = excluded.published_at,
    publish_error = excluded.publish_error,
    publish_attempts = messages.publish_attempts + excluded.publish_attempts,
    last_publish_attempt_at = excluded.last_publish_attempt_at,
    defer_reason = excluded.defer_reason,
    defer_attempts = messages.defer_attempts + excluded.defer_attempts,
    last_defer_attempt_at = excluded.last_defer_attempt_at,
    deleted_at = excluded.deleted_at,
    deleted_seq = excluded.deleted_seq,
    deleted_reason = excluded.deleted_reason;

-- name: CountMessages :one
SELECT COUNT(*) FROM messages;

-- name: CountPublishedMessages :one
SELECT COUNT(*) FROM messages WHERE ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> '';

-- name: CountPublishFailures :one
SELECT COUNT(*) FROM messages WHERE message_state = 'failed';

-- name: CountDeferredMessages :one
SELECT COUNT(*) FROM messages WHERE message_state = 'deferred';

-- name: GetRecentMessages :many
SELECT * FROM messages
ORDER BY created_at DESC
LIMIT :limit;

-- name: GetPublishFailures :many
SELECT * FROM messages
WHERE message_state IN ('failed', 'deferred')
ORDER BY COALESCE(last_publish_attempt_at, last_defer_attempt_at, created_at) DESC
LIMIT :limit;

-- name: CountMessagesByDID :one
SELECT COUNT(*) FROM messages WHERE at_did = :at_did;

-- name: CountDeletedMessages :one
SELECT COUNT(*) FROM messages WHERE message_state = 'deleted';

-- name: ListRecentPublishedMessagesByDID :many
SELECT * FROM messages
WHERE at_did = :at_did AND ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> ''
ORDER BY published_at DESC
LIMIT :limit;

-- name: ResetMessageForRetry :exec
UPDATE messages SET
  message_state = 'pending',
  publish_error = '',
  publish_attempts = 0,
  last_publish_attempt_at = NULL
WHERE at_uri = :at_uri;

-- name: ListPublishedMessagesGlobal :many
SELECT * FROM messages
WHERE ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> ''
ORDER BY published_at DESC
LIMIT :limit;

-- name: ListMessageTypes :many
SELECT DISTINCT type FROM messages ORDER BY type;

-- name: ListTopDeferredReasons :many
SELECT defer_reason as reason, COUNT(*) as count
FROM messages
WHERE message_state = 'deferred' AND defer_reason IS NOT NULL AND defer_reason <> ''
GROUP BY defer_reason
ORDER BY count DESC
LIMIT :limit;

-- name: ListTopIssueAccounts :many
SELECT
  ba.at_did,
  ba.ssb_feed_id,
  ba.active,
  COALESCE(s.total_messages, 0) as total_messages,
  COALESCE(s.issue_messages, 0) as issue_messages,
  COALESCE(s.failed_messages, 0) as failed_messages,
  COALESCE(s.deferred_messages, 0) as deferred_count
FROM bridged_accounts ba
LEFT JOIN (
  SELECT
    at_did,
    COUNT(*) as total_messages,
    SUM(CASE WHEN message_state IN ('failed', 'deferred') THEN 1 ELSE 0 END) as issue_messages,
    SUM(CASE WHEN message_state = 'failed' THEN 1 ELSE 0 END) as failed_messages,
    SUM(CASE WHEN message_state = 'deferred' THEN 1 ELSE 0 END) as deferred_messages
  FROM messages
  GROUP BY at_did
) s ON s.at_did = ba.at_did
WHERE s.issue_messages > 0
ORDER BY s.issue_messages DESC
LIMIT :limit;

-- name: GetRetryCandidates :many
SELECT * FROM messages
WHERE message_state = 'failed'
AND (:at_did = '' OR at_did = :at_did)
AND publish_attempts < :max_attempts
ORDER BY last_publish_attempt_at ASC
LIMIT :limit;

-- name: GetDeferredCandidates :many
SELECT * FROM messages
WHERE message_state = 'deferred'
ORDER BY last_defer_attempt_at ASC
LIMIT :limit;

-- name: GetLatestDeferredReason :one
SELECT defer_reason FROM messages
WHERE message_state = 'deferred' AND defer_reason IS NOT NULL AND defer_reason <> ''
ORDER BY created_at DESC
LIMIT 1;
