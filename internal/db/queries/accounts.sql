-- name: GetBridgedAccount :one
SELECT * FROM bridged_accounts WHERE at_did = :at_did;

-- name: AddBridgedAccount :exec
INSERT INTO bridged_accounts (at_did, ssb_feed_id, active)
VALUES (:at_did, :ssb_feed_id, :active)
ON CONFLICT(at_did) DO UPDATE SET
    ssb_feed_id = excluded.ssb_feed_id,
    active = excluded.active;

-- name: GetAllBridgedAccounts :many
SELECT * FROM bridged_accounts ORDER BY created_at DESC;

-- name: ListActiveBridgedAccounts :many
SELECT * FROM bridged_accounts WHERE active = 1 ORDER BY created_at DESC;

-- name: CountBridgedAccounts :one
SELECT COUNT(*) FROM bridged_accounts;

-- name: CountActiveBridgedAccounts :one
SELECT COUNT(*) FROM bridged_accounts WHERE active = 1;

-- name: ListActiveBridgedAccountsWithStats :many
SELECT
  ba.at_did,
  ba.ssb_feed_id,
  ba.created_at,
  ba.active,
  COALESCE(s.total_messages, 0) as total_messages,
  COALESCE(s.published_messages, 0) as published_messages,
  COALESCE(s.failed_messages, 0) as failed_messages,
  COALESCE(s.deferred_messages, 0) as deferred_messages,
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
WHERE ba.active = 1
ORDER BY ba.created_at DESC;

-- name: ListBridgedAccountsWithStats :many
SELECT
  ba.at_did,
  ba.ssb_feed_id,
  ba.created_at,
  ba.active,
  COALESCE(s.total_messages, 0) as total_messages,
  COALESCE(s.published_messages, 0) as published_messages,
  COALESCE(s.failed_messages, 0) as failed_messages,
  COALESCE(s.deferred_messages, 0) as deferred_messages,
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
ORDER BY ba.created_at DESC;

-- name: ListActiveBridgedAccountsWithStatsSorted :many
SELECT
  ba.at_did,
  ba.ssb_feed_id,
  ba.created_at,
  ba.active,
  COALESCE(s.total_messages, 0) as total_messages,
  COALESCE(s.published_messages, 0) as published_messages,
  COALESCE(s.failed_messages, 0) as failed_messages,
  COALESCE(s.deferred_messages, 0) as deferred_messages,
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
WHERE ba.active = 1
ORDER BY ba.created_at DESC;

-- name: GetActiveBridgedAccountWithStats :one
SELECT
  ba.at_did,
  ba.ssb_feed_id,
  ba.created_at,
  ba.active,
  COALESCE(s.total_messages, 0) as total_messages,
  COALESCE(s.published_messages, 0) as published_messages,
  COALESCE(s.failed_messages, 0) as failed_messages,
  COALESCE(s.deferred_messages, 0) as deferred_messages,
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
WHERE ba.active = 1 AND ba.at_did = :at_did;
