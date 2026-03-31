-- internal/db/schema.sql

CREATE TABLE IF NOT EXISTS bridged_accounts (
    at_did TEXT PRIMARY KEY,
    ssb_feed_id TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    active BOOLEAN DEFAULT 1
);

CREATE TABLE IF NOT EXISTS messages (
    at_uri TEXT PRIMARY KEY,
    at_cid TEXT NOT NULL,
    ssb_msg_ref TEXT,
    at_did TEXT NOT NULL,
    type TEXT NOT NULL,
    message_state TEXT NOT NULL DEFAULT 'pending',
    raw_at_json TEXT,
    raw_ssb_json TEXT,
    published_at DATETIME,
    publish_error TEXT,
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_publish_attempt_at DATETIME,
    defer_reason TEXT,
    defer_attempts INTEGER NOT NULL DEFAULT 0,
    last_defer_attempt_at DATETIME,
    deleted_at DATETIME,
    deleted_seq INTEGER,
    deleted_reason TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(at_did) REFERENCES bridged_accounts(at_did)
);

CREATE INDEX IF NOT EXISTS idx_messages_at_did ON messages(at_did);
CREATE INDEX IF NOT EXISTS idx_messages_type ON messages(type);
CREATE INDEX IF NOT EXISTS idx_messages_state ON messages(message_state);

CREATE TABLE IF NOT EXISTS blobs (
    at_cid TEXT PRIMARY KEY,
    ssb_blob_ref TEXT NOT NULL,
    size INTEGER,
    mime_type TEXT,
    downloaded_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bridge_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS known_peers (
    addr TEXT PRIMARY KEY,
    pubkey BLOB NOT NULL,
    last_seen DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
