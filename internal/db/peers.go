package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type KnownPeer struct {
	Addr      string
	PubKey    []byte
	LastSeen  *time.Time
	CreatedAt time.Time
}

func (db *DB) AddKnownPeer(ctx context.Context, p KnownPeer) error {
	_, err := db.conn.ExecContext(
		ctx,
		`INSERT INTO known_peers (addr, pubkey, last_seen)
		 VALUES (?, ?, ?)
		 ON CONFLICT(addr) DO UPDATE SET pubkey=excluded.pubkey, last_seen=excluded.last_seen`,
		p.Addr, p.PubKey, p.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("add known peer %s: %w", p.Addr, err)
	}
	return nil
}

func (db *DB) AddKnownPeerAddr(ctx context.Context, addr string, pubKey []byte) error {
	return db.AddKnownPeer(ctx, KnownPeer{
		Addr:   addr,
		PubKey: pubKey,
	})
}

func (db *DB) GetKnownPeers(ctx context.Context) ([]KnownPeer, error) {
	return querySlice(ctx, db.conn,
		"list known peers",
		`SELECT addr, pubkey, last_seen, created_at FROM known_peers ORDER BY created_at DESC`,
		nil, scanKnownPeerRow,
	)
}

func scanKnownPeerRow(rows *sql.Rows) (KnownPeer, error) {
	var p KnownPeer
	var lastSeen sql.NullTime
	err := rows.Scan(&p.Addr, &p.PubKey, &lastSeen, &p.CreatedAt)
	if lastSeen.Valid {
		p.LastSeen = &lastSeen.Time
	}
	return p, err
}
