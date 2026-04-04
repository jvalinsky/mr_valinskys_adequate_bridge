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

CREATE TABLE IF NOT EXISTS atproto_sources (
    source_key TEXT PRIMARY KEY,
    relay_url TEXT NOT NULL,
    last_seq INTEGER NOT NULL DEFAULT 0,
    connected_at DATETIME,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS atproto_repos (
    did TEXT PRIMARY KEY,
    tracking BOOLEAN NOT NULL DEFAULT 1,
    reason TEXT,
    sync_state TEXT NOT NULL DEFAULT 'pending',
    generation INTEGER NOT NULL DEFAULT 0,
    current_rev TEXT,
    current_commit_cid TEXT,
    current_data_cid TEXT,
    last_firehose_seq INTEGER,
    last_backfill_at DATETIME,
    last_event_cursor INTEGER,
    handle TEXT,
    pds_url TEXT,
    account_active BOOLEAN,
    account_status TEXT,
    last_identity_at DATETIME,
    last_account_at DATETIME,
    last_error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_atproto_repos_state ON atproto_repos(sync_state);
CREATE INDEX IF NOT EXISTS idx_atproto_repos_tracking ON atproto_repos(tracking);

CREATE TABLE IF NOT EXISTS atproto_commit_buffer (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    did TEXT NOT NULL,
    generation INTEGER NOT NULL,
    rev TEXT NOT NULL,
    seq INTEGER NOT NULL DEFAULT 0,
    raw_event_json TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(did, generation, rev)
);

CREATE INDEX IF NOT EXISTS idx_atproto_commit_buffer_repo ON atproto_commit_buffer(did, generation, seq, rev);

CREATE TABLE IF NOT EXISTS atproto_records (
    did TEXT NOT NULL,
    collection TEXT NOT NULL,
    rkey TEXT NOT NULL,
    at_uri TEXT NOT NULL UNIQUE,
    at_cid TEXT NOT NULL,
    record_json TEXT NOT NULL,
    last_rev TEXT,
    last_seq INTEGER,
    deleted BOOLEAN NOT NULL DEFAULT 0,
    deleted_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(did, collection, rkey)
);

CREATE INDEX IF NOT EXISTS idx_atproto_records_did_collection ON atproto_records(did, collection, rkey);
CREATE INDEX IF NOT EXISTS idx_atproto_records_uri ON atproto_records(at_uri);

CREATE TABLE IF NOT EXISTS atproto_event_log (
    cursor INTEGER PRIMARY KEY AUTOINCREMENT,
    did TEXT NOT NULL,
    collection TEXT NOT NULL,
    rkey TEXT NOT NULL,
    at_uri TEXT NOT NULL,
    at_cid TEXT,
    action TEXT NOT NULL,
    live BOOLEAN NOT NULL DEFAULT 1,
    rev TEXT,
    seq INTEGER,
    record_json TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_atproto_event_log_cursor ON atproto_event_log(cursor);
CREATE INDEX IF NOT EXISTS idx_atproto_event_log_repo ON atproto_event_log(did, cursor);

CREATE TABLE IF NOT EXISTS known_peers (
    addr TEXT PRIMARY KEY,
    pubkey BLOB NOT NULL,
    last_seen DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssb_follower_sync (
    bot_did TEXT NOT NULL,
    follower_ssb_feed TEXT NOT NULL,
    followed_back_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (bot_did, follower_ssb_feed)
);

CREATE INDEX IF NOT EXISTS idx_follower_sync_bot ON ssb_follower_sync(bot_did);
CREATE INDEX IF NOT EXISTS idx_follower_sync_follower ON ssb_follower_sync(follower_ssb_feed);
