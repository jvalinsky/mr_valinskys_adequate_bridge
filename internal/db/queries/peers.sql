-- name: AddKnownPeer :exec
INSERT INTO known_peers (addr, pubkey, last_seen)
VALUES (?, ?, ?)
ON CONFLICT(addr) DO UPDATE SET pubkey=excluded.pubkey, last_seen=excluded.last_seen;

-- name: GetKnownPeers :many
SELECT addr, pubkey, last_seen, created_at FROM known_peers ORDER BY created_at DESC;