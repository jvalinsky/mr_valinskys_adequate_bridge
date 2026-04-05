package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sqlc "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db/sqlc"
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
	rows, err := db.Queries().GetKnownPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("get known peers: %w", err)
	}
	result := make([]KnownPeer, len(rows))
	for i, r := range rows {
		result[i] = convertKnownPeer(r)
	}
	return result, nil
}

func convertKnownPeer(r sqlc.KnownPeer) KnownPeer {
	return KnownPeer{
		Addr:      r.Addr,
		PubKey:    r.Pubkey,
		LastSeen:  nilOrTime(r.LastSeen),
		CreatedAt: r.CreatedAt.Time,
	}
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
