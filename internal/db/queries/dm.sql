-- name: SaveDM :exec
INSERT OR REPLACE INTO direct_messages (
	ssb_msg_key, ssb_msg_seq, sender_feed, recipient_feed,
	encrypted_content, plaintext, decrypted_at, created_at, received_at, is_outbound
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDMByKey :one
SELECT id, ssb_msg_key, ssb_msg_seq, sender_feed, recipient_feed,
	   encrypted_content, plaintext, decrypted_at, created_at, received_at, is_outbound
FROM direct_messages
WHERE ssb_msg_key = ?;

-- name: ListDMsForFeed :many
SELECT id, ssb_msg_key, ssb_msg_seq, sender_feed, recipient_feed,
	   encrypted_content, plaintext, decrypted_at, created_at, received_at, is_outbound
FROM direct_messages
WHERE sender_feed = ? OR recipient_feed = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListDMConversations :many
SELECT DISTINCT 
	CASE 
		WHEN sender_feed = ? THEN recipient_feed 
		ELSE sender_feed 
	END as other_party
FROM direct_messages
WHERE sender_feed = ? OR recipient_feed = ?
ORDER BY MAX(created_at) DESC;

-- name: UpdateDMDecrypted :exec
UPDATE direct_messages 
SET plaintext = ?, decrypted_at = ?
WHERE ssb_msg_key = ?;