-- Migration 002: Reverse sync persistence
-- Created: 2026-04-07

CREATE TABLE IF NOT EXISTS reverse_identity_mappings (
    ssb_feed_id TEXT PRIMARY KEY,
    at_did TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT 1,
    allow_posts BOOLEAN NOT NULL DEFAULT 1,
    allow_replies BOOLEAN NOT NULL DEFAULT 1,
    allow_follows BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_reverse_identity_mappings_at_did ON reverse_identity_mappings(at_did);
CREATE INDEX IF NOT EXISTS idx_reverse_identity_mappings_active ON reverse_identity_mappings(active);

CREATE TABLE IF NOT EXISTS reverse_events (
    source_ssb_msg_ref TEXT PRIMARY KEY,
    source_ssb_author TEXT NOT NULL,
    source_ssb_seq INTEGER,
    receive_log_seq INTEGER NOT NULL,
    at_did TEXT NOT NULL,
    action TEXT NOT NULL,
    event_state TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at DATETIME,
    published_at DATETIME,
    error_text TEXT,
    defer_reason TEXT,
    target_ssb_ref TEXT,
    target_ssb_feed_id TEXT,
    target_at_did TEXT,
    target_at_uri TEXT,
    target_at_cid TEXT,
    result_at_uri TEXT,
    result_at_cid TEXT,
    result_collection TEXT,
    raw_ssb_json TEXT,
    raw_at_json TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_reverse_events_receive_log_seq ON reverse_events(receive_log_seq);
CREATE INDEX IF NOT EXISTS idx_reverse_events_at_did ON reverse_events(at_did);
CREATE INDEX IF NOT EXISTS idx_reverse_events_state ON reverse_events(event_state);
CREATE INDEX IF NOT EXISTS idx_reverse_events_action ON reverse_events(action);
CREATE INDEX IF NOT EXISTS idx_reverse_events_target_at_did ON reverse_events(target_at_did);
