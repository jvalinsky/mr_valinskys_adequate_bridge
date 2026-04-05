-- name: GetBridgeState :one
SELECT key, value, updated_at FROM bridge_state WHERE key = ?;

-- name: SetBridgeState :exec
INSERT INTO bridge_state (key, value, updated_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP;

-- name: GetAllBridgeState :many
SELECT key, value, updated_at FROM bridge_state ORDER BY key ASC;

-- name: GetBridgeHealth :one
SELECT value FROM bridge_state WHERE key = 'bridge_runtime_status';

-- name: GetBridgeHeartbeat :one
SELECT value FROM bridge_state WHERE key = 'bridge_runtime_last_heartbeat_at';