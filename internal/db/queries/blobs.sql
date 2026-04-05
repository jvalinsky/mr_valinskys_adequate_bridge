-- name: GetBlob :one
SELECT * FROM blobs WHERE at_cid = :at_cid;

-- name: GetBlobBySSBRef :one
SELECT * FROM blobs WHERE ssb_blob_ref = :ssb_blob_ref;

-- name: AddBlob :exec
INSERT INTO blobs (at_cid, ssb_blob_ref, size, mime_type)
VALUES (:at_cid, :ssb_blob_ref, :size, :mime_type)
ON CONFLICT(at_cid) DO UPDATE SET
    ssb_blob_ref = excluded.ssb_blob_ref,
    size = excluded.size,
    mime_type = excluded.mime_type;

-- name: DeleteBlob :exec
DELETE FROM blobs WHERE at_cid = :at_cid;

-- name: CountBlobs :one
SELECT COUNT(*) FROM blobs;

-- name: CountBlobSize :one
SELECT COALESCE(SUM(size), 0) FROM blobs;

-- name: GetRecentBlobs :many
SELECT * FROM blobs
ORDER BY downloaded_at DESC
LIMIT :limit;

-- name: GetBlobsOlderThan :many
SELECT * FROM blobs
WHERE downloaded_at < datetime('now', '-' || :days || ' days')
ORDER BY downloaded_at ASC;
