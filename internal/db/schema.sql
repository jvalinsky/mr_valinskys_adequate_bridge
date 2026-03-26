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
    raw_at_json TEXT,
    raw_ssb_json TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(at_did) REFERENCES bridged_accounts(at_did)
);

CREATE INDEX IF NOT EXISTS idx_messages_at_did ON messages(at_did);
CREATE INDEX IF NOT EXISTS idx_messages_type ON messages(type);

CREATE TABLE IF NOT EXISTS blobs (
    at_cid TEXT PRIMARY KEY,
    ssb_blob_ref TEXT NOT NULL,
    size INTEGER,
    mime_type TEXT,
    downloaded_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
