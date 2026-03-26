package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	conn *sql.DB
}

type BridgedAccount struct {
	ATDID     string
	SSBFeedID string
	CreatedAt time.Time
	Active    bool
}

type Message struct {
	ATURI      string
	ATCID      string
	SSBMsgRef  string
	ATDID      string
	Type       string
	RawATJson  string
	RawSSBJson string
	CreatedAt  time.Time
}

func Open(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) initSchema() error {
	_, err := db.conn.Exec(schemaSQL)
	return err
}

func (db *DB) AddBridgedAccount(ctx context.Context, acc BridgedAccount) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO bridged_accounts (at_did, ssb_feed_id, active) VALUES (?, ?, ?)
		 ON CONFLICT(at_did) DO UPDATE SET ssb_feed_id=excluded.ssb_feed_id, active=excluded.active`,
		acc.ATDID, acc.SSBFeedID, acc.Active,
	)
	return err
}

func (db *DB) GetBridgedAccount(ctx context.Context, atDID string) (*BridgedAccount, error) {
	var acc BridgedAccount
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts WHERE at_did = ?`,
		atDID,
	).Scan(&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &acc, nil
}

func (db *DB) GetAllBridgedAccounts(ctx context.Context) ([]BridgedAccount, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT at_did, ssb_feed_id, created_at, active FROM bridged_accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []BridgedAccount
	for rows.Next() {
		var acc BridgedAccount
		if err := rows.Scan(&acc.ATDID, &acc.SSBFeedID, &acc.CreatedAt, &acc.Active); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

func (db *DB) AddMessage(ctx context.Context, msg Message) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO messages (at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(at_uri) DO UPDATE SET ssb_msg_ref=excluded.ssb_msg_ref, raw_ssb_json=excluded.raw_ssb_json`,
		msg.ATURI, msg.ATCID, msg.SSBMsgRef, msg.ATDID, msg.Type, msg.RawATJson, msg.RawSSBJson,
	)
	return err
}

func (db *DB) GetMessage(ctx context.Context, atURI string) (*Message, error) {
	var msg Message
	var ssbMsgRef, rawATJson, rawSSBJson sql.NullString
	err := db.conn.QueryRowContext(
		ctx,
		`SELECT at_uri, at_cid, ssb_msg_ref, at_did, type, raw_at_json, raw_ssb_json, created_at FROM messages WHERE at_uri = ?`,
		atURI,
	).Scan(&msg.ATURI, &msg.ATCID, &ssbMsgRef, &msg.ATDID, &msg.Type, &rawATJson, &rawSSBJson, &msg.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	msg.SSBMsgRef = ssbMsgRef.String
	msg.RawATJson = rawATJson.String
	msg.RawSSBJson = rawSSBJson.String
	return &msg, nil
}
