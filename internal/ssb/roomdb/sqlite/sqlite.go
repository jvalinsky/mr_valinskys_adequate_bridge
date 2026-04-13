package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("roomdb: open db: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("roomdb: ping db: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("roomdb: migrate: %w", err)
	}

	return db, nil
}

func (db *DB) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS members (
  id            INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  role          INTEGER NOT NULL,
  pub_key       TEXT    NOT NULL UNIQUE,
  CHECK(role > 0)
);
CREATE INDEX IF NOT EXISTS members_pubkeys ON members(pub_key);

CREATE TABLE IF NOT EXISTS fallback_passwords (
  id            INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  login         TEXT    NOT NULL UNIQUE,
  password_hash BLOB    NOT NULL,
  member_id     INTEGER NOT NULL,
  FOREIGN KEY ( member_id ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS fallback_passwords_by_login ON fallback_passwords(login);

CREATE TABLE IF NOT EXISTS invites (
  id               INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  hashed_token     TEXT UNIQUE NOT NULL,
  created_by       INTEGER NOT NULL,
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  active boolean   NOT NULL DEFAULT TRUE,
  FOREIGN KEY ( created_by ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS invite_active_ids ON invites(id) WHERE active=TRUE;
CREATE UNIQUE INDEX IF NOT EXISTS invite_active_tokens ON invites(hashed_token) WHERE active=TRUE;
CREATE INDEX IF NOT EXISTS invite_inactive ON invites(active);

CREATE TABLE IF NOT EXISTS aliases (
  id            INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  name          TEXT UNIQUE NOT NULL,
  member_id     INTEGER NOT NULL,
  signature     BLOB NOT NULL,
  FOREIGN KEY ( member_id ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS aliases_ids ON aliases(id);
CREATE UNIQUE INDEX IF NOT EXISTS aliases_names ON aliases(name);

CREATE TABLE IF NOT EXISTS denied_keys (
  id          INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  pub_key     TEXT NOT NULL UNIQUE,
  comment     TEXT NOT NULL,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS denied_keys_by_pubkey ON denied_keys(pub_key);

CREATE TABLE IF NOT EXISTS room_config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runtime_attendants (
  feed_id      TEXT PRIMARY KEY,
  addr         TEXT NOT NULL,
  connected_at DATETIME NOT NULL,
  last_seen_at DATETIME NOT NULL,
  active       BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX IF NOT EXISTS runtime_attendants_active ON runtime_attendants(active);
CREATE INDEX IF NOT EXISTS runtime_attendants_last_seen ON runtime_attendants(last_seen_at);

CREATE TABLE IF NOT EXISTS runtime_tunnel_endpoints (
  target_feed  TEXT PRIMARY KEY,
  addr         TEXT NOT NULL,
  announced_at DATETIME NOT NULL,
  last_seen_at DATETIME NOT NULL,
  active       BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX IF NOT EXISTS runtime_tunnel_endpoints_active ON runtime_tunnel_endpoints(active);
CREATE INDEX IF NOT EXISTS runtime_tunnel_endpoints_last_seen ON runtime_tunnel_endpoints(last_seen_at);

CREATE TABLE IF NOT EXISTS auth_tokens (
  id         INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  token      TEXT UNIQUE NOT NULL,
  member_id  INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_used_at DATETIME,
  rotation_count INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY ( member_id ) REFERENCES members( "id" )  ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS auth_tokens_by_token ON auth_tokens(token);
CREATE INDEX IF NOT EXISTS auth_tokens_by_member ON auth_tokens(member_id);
`
	_, err := db.conn.Exec(schema)
	return err
}

func (db *DB) Close() error {
	return db.conn.Close()
}

type Members struct {
	db *DB
}

func (db *DB) Members() roomdb.MembersService {
	return &Members{db: db}
}

func (m *Members) Add(ctx context.Context, pubKey refs.FeedRef, role roomdb.Role) (int64, error) {
	result, err := m.db.conn.ExecContext(ctx,
		"INSERT INTO members (pub_key, role) VALUES (?, ?)",
		pubKey.String(), role)
	if err != nil {
		return 0, fmt.Errorf("members add: %w", err)
	}
	return result.LastInsertId()
}

func (m *Members) GetByID(ctx context.Context, id int64) (roomdb.Member, error) {
	var member roomdb.Member
	var pubKeyStr string
	var role int
	err := m.db.conn.QueryRowContext(ctx,
		"SELECT id, pub_key, role FROM members WHERE id = ?", id).
		Scan(&member.ID, &pubKeyStr, &role)
	if err != nil {
		return member, fmt.Errorf("members get by id: %w", err)
	}
	ref, err := refs.ParseFeedRef(pubKeyStr)
	if err != nil {
		return member, fmt.Errorf("members parse ref: %w", err)
	}
	member.PubKey = *ref
	member.Role = roomdb.Role(role)
	return member, nil
}

func (m *Members) GetByFeed(ctx context.Context, feed refs.FeedRef) (roomdb.Member, error) {
	var member roomdb.Member
	var id int64
	var role int
	err := m.db.conn.QueryRowContext(ctx,
		"SELECT id, pub_key, role FROM members WHERE pub_key = ?", feed.String()).
		Scan(&id, new(string), &role)
	if err != nil {
		return member, fmt.Errorf("members get by feed: %w", err)
	}
	member.ID = id
	member.PubKey = feed
	member.Role = roomdb.Role(role)
	return member, nil
}

func (m *Members) List(ctx context.Context) ([]roomdb.Member, error) {
	rows, err := m.db.conn.QueryContext(ctx,
		"SELECT id, pub_key, role FROM members ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("members list: %w", err)
	}
	defer rows.Close()

	var members []roomdb.Member
	for rows.Next() {
		var member roomdb.Member
		var pubKeyStr string
		var role int
		if err := rows.Scan(&member.ID, &pubKeyStr, &role); err != nil {
			return nil, fmt.Errorf("members scan: %w", err)
		}
		ref, err := refs.ParseFeedRef(pubKeyStr)
		if err != nil {
			continue
		}
		member.PubKey = *ref
		member.Role = roomdb.Role(role)
		members = append(members, member)
	}
	return members, nil
}

func (m *Members) Count(ctx context.Context) (uint, error) {
	var count uint
	err := m.db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM members").Scan(&count)
	return count, err
}

func (m *Members) RemoveFeed(ctx context.Context, feed refs.FeedRef) error {
	_, err := m.db.conn.ExecContext(ctx,
		"DELETE FROM members WHERE pub_key = ?", feed.String())
	return err
}

func (m *Members) RemoveID(ctx context.Context, id int64) error {
	_, err := m.db.conn.ExecContext(ctx,
		"DELETE FROM members WHERE id = ?", id)
	return err
}

func (m *Members) SetRole(ctx context.Context, id int64, role roomdb.Role) error {
	_, err := m.db.conn.ExecContext(ctx,
		"UPDATE members SET role = ? WHERE id = ?", role, id)
	return err
}

type Aliases struct {
	db *DB
}

func (db *DB) Aliases() roomdb.AliasesService {
	return &Aliases{db: db}
}

func (a *Aliases) Resolve(ctx context.Context, alias string) (roomdb.Alias, error) {
	var al roomdb.Alias
	var memberID int64
	var sig []byte
	var ownerPubKey string
	err := a.db.conn.QueryRowContext(ctx,
		`SELECT aliases.id, aliases.name, aliases.member_id, aliases.signature, members.pub_key 
		 FROM aliases JOIN members ON aliases.member_id = members.id 
		 WHERE aliases.name = ?`, alias).
		Scan(&al.ID, &al.Name, &memberID, &sig, &ownerPubKey)
	if err != nil {
		return al, fmt.Errorf("aliases resolve: %w", err)
	}
	al.Signature = sig
	ownerRef, err := refs.ParseFeedRef(ownerPubKey)
	if err != nil {
		return al, fmt.Errorf("aliases parse owner: %w", err)
	}
	al.Owner = *ownerRef
	al.ReversePTR = ownerPubKey
	return al, nil
}

func (a *Aliases) GetByID(ctx context.Context, id int64) (roomdb.Alias, error) {
	var al roomdb.Alias
	var memberID int64
	var sig []byte
	var ownerPubKey string
	err := a.db.conn.QueryRowContext(ctx,
		`SELECT aliases.id, aliases.name, aliases.member_id, aliases.signature, members.pub_key 
		 FROM aliases JOIN members ON aliases.member_id = members.id 
		 WHERE aliases.id = ?`, id).
		Scan(&al.ID, &al.Name, &memberID, &sig, &ownerPubKey)
	if err != nil {
		return al, fmt.Errorf("aliases get by id: %w", err)
	}
	al.Signature = sig
	ownerRef, err := refs.ParseFeedRef(ownerPubKey)
	if err != nil {
		return al, fmt.Errorf("aliases parse owner: %w", err)
	}
	al.Owner = *ownerRef
	al.ReversePTR = ownerPubKey
	return al, nil
}

func (a *Aliases) List(ctx context.Context) ([]roomdb.Alias, error) {
	rows, err := a.db.conn.QueryContext(ctx,
		`SELECT aliases.id, aliases.name, aliases.member_id, aliases.signature, members.pub_key 
		 FROM aliases JOIN members ON aliases.member_id = members.id 
		 ORDER BY aliases.name`)
	if err != nil {
		return nil, fmt.Errorf("aliases list: %w", err)
	}
	defer rows.Close()

	var aliases []roomdb.Alias
	for rows.Next() {
		var al roomdb.Alias
		var memberID int64
		var sig []byte
		var ownerPubKey string
		if err := rows.Scan(&al.ID, &al.Name, &memberID, &sig, &ownerPubKey); err != nil {
			continue
		}
		al.Signature = sig
		ownerRef, err := refs.ParseFeedRef(ownerPubKey)
		if err != nil {
			continue
		}
		al.Owner = *ownerRef
		al.ReversePTR = ownerPubKey
		aliases = append(aliases, al)
	}
	return aliases, nil
}

func (a *Aliases) Register(ctx context.Context, alias string, userFeed refs.FeedRef, signature []byte) error {
	members := &Members{db: a.db}
	member, err := members.GetByFeed(ctx, userFeed)
	if err != nil {
		return fmt.Errorf("aliases register: get member: %w", err)
	}

	_, err = a.db.conn.ExecContext(ctx,
		"INSERT INTO aliases (name, member_id, signature) VALUES (?, ?, ?)",
		strings.ToLower(alias), member.ID, signature)
	return err
}

func (a *Aliases) Revoke(ctx context.Context, alias string) error {
	_, err := a.db.conn.ExecContext(ctx,
		"DELETE FROM aliases WHERE name = ?", strings.ToLower(alias))
	return err
}

type Invites struct {
	db *DB
}

func (db *DB) Invites() roomdb.InvitesService {
	return &Invites{db: db}
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(h[:])
}

func (i *Invites) Create(ctx context.Context, createdBy int64) (string, error) {
	token := fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomToken())
	hashed := hashToken(token)

	result, err := i.db.conn.ExecContext(ctx,
		"INSERT INTO invites (hashed_token, created_by, created_at) VALUES (?, ?, ?)",
		hashed, createdBy, time.Now())
	if err != nil {
		return "", fmt.Errorf("invites create: %w", err)
	}
	_, _ = result.LastInsertId()
	return token, nil
}

func (i *Invites) Consume(ctx context.Context, token string, newMember refs.FeedRef) (roomdb.Invite, error) {
	hashed := hashToken(token)

	var invite roomdb.Invite
	var createdBy int64
	var hashedToken string
	var createdAt time.Time
	err := i.db.conn.QueryRowContext(ctx,
		"SELECT id, hashed_token, created_by, created_at, active FROM invites WHERE hashed_token = ?",
		hashed).Scan(&invite.ID, &hashedToken, &createdBy, &createdAt, &invite.Active)
	if err != nil {
		return invite, fmt.Errorf("invites consume: %w", err)
	}

	invite.CreatedAt = createdAt.Unix()

	if !invite.Active {
		return invite, fmt.Errorf("invite already used")
	}

	members := &Members{db: i.db}
	member, err := members.GetByFeed(ctx, newMember)
	if err != nil {
		memberID, err := members.Add(ctx, newMember, roomdb.RoleMember)
		if err != nil {
			return invite, fmt.Errorf("invites consume: add member: %w", err)
		}
		member.ID = memberID
	}

	_, err = i.db.conn.ExecContext(ctx,
		"UPDATE invites SET active = FALSE WHERE id = ?", invite.ID)
	if err != nil {
		return invite, fmt.Errorf("invites consume: deactivate: %w", err)
	}

	invite.Active = false
	invite.UsedBy = &newMember
	invite.UsedAt = time.Now().Unix()

	members.SetRole(ctx, member.ID, roomdb.RoleMember)

	return invite, nil
}

func (i *Invites) GetByToken(ctx context.Context, token string) (roomdb.Invite, error) {
	var invite roomdb.Invite
	var hashedToken string
	var createdAt time.Time
	err := i.db.conn.QueryRowContext(ctx,
		"SELECT id, hashed_token, created_by, created_at, active FROM invites WHERE hashed_token = ?",
		hashToken(token)).
		Scan(&invite.ID, &hashedToken, &invite.CreatedBy, &createdAt, &invite.Active)
	if err != nil {
		return invite, fmt.Errorf("invites get by token: %w", err)
	}
	invite.CreatedAt = createdAt.Unix()
	return invite, nil
}

func (i *Invites) GetByID(ctx context.Context, id int64) (roomdb.Invite, error) {
	var invite roomdb.Invite
	var hashedToken string
	var createdAt time.Time
	err := i.db.conn.QueryRowContext(ctx,
		"SELECT id, hashed_token, created_by, created_at, active FROM invites WHERE id = ?", id).
		Scan(&invite.ID, &hashedToken, &invite.CreatedBy, &createdAt, &invite.Active)
	if err != nil {
		return invite, fmt.Errorf("invites get by id: %w", err)
	}
	invite.CreatedAt = createdAt.Unix()
	return invite, nil
}

func (i *Invites) List(ctx context.Context) ([]roomdb.Invite, error) {
	rows, err := i.db.conn.QueryContext(ctx,
		"SELECT id, hashed_token, created_by, created_at, active FROM invites ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("invites list: %w", err)
	}
	defer rows.Close()

	var invites []roomdb.Invite
	for rows.Next() {
		var invite roomdb.Invite
		var hashedToken string
		var createdAt time.Time
		if err := rows.Scan(&invite.ID, &hashedToken, &invite.CreatedBy, &createdAt, &invite.Active); err != nil {
			continue
		}
		invite.CreatedAt = createdAt.Unix()
		invites = append(invites, invite)
	}
	return invites, nil
}

func (i *Invites) Count(ctx context.Context, onlyActive bool) (uint, error) {
	var count uint
	var query string
	if onlyActive {
		query = "SELECT COUNT(*) FROM invites WHERE active = TRUE"
	} else {
		query = "SELECT COUNT(*) FROM invites"
	}
	err := i.db.conn.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

func (i *Invites) Revoke(ctx context.Context, id int64) error {
	_, err := i.db.conn.ExecContext(ctx,
		"UPDATE invites SET active = FALSE WHERE id = ?", id)
	return err
}

type DeniedKeys struct {
	db *DB
}

func (db *DB) DeniedKeys() roomdb.DeniedKeysService {
	return &DeniedKeys{db: db}
}

func (d *DeniedKeys) Add(ctx context.Context, ref refs.FeedRef, comment string) error {
	_, err := d.db.conn.ExecContext(ctx,
		"INSERT INTO denied_keys (pub_key, comment, created_at) VALUES (?, ?, ?)",
		ref.String(), comment, time.Now())
	return err
}

func (d *DeniedKeys) HasFeed(ctx context.Context, ref refs.FeedRef) bool {
	var exists bool
	d.db.conn.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM denied_keys WHERE pub_key = ?)", ref.String()).Scan(&exists)
	return exists
}

func (d *DeniedKeys) HasID(ctx context.Context, id int64) bool {
	var exists bool
	d.db.conn.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM denied_keys WHERE id = ?)", id).Scan(&exists)
	return exists
}

func (d *DeniedKeys) GetByID(ctx context.Context, id int64) (roomdb.ListEntry, error) {
	var entry roomdb.ListEntry
	var pubKeyStr string
	err := d.db.conn.QueryRowContext(ctx,
		"SELECT id, pub_key, comment, created_at FROM denied_keys WHERE id = ?", id).
		Scan(&entry.ID, &pubKeyStr, &entry.Comment, &entry.AddedAt)
	if err != nil {
		return entry, fmt.Errorf("denied keys get by id: %w", err)
	}
	ref, err := refs.ParseFeedRef(pubKeyStr)
	if err != nil {
		return entry, fmt.Errorf("denied keys parse ref: %w", err)
	}
	entry.PubKey = *ref
	return entry, nil
}

func (d *DeniedKeys) List(ctx context.Context) ([]roomdb.ListEntry, error) {
	rows, err := d.db.conn.QueryContext(ctx,
		"SELECT id, pub_key, comment, created_at FROM denied_keys ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("denied keys list: %w", err)
	}
	defer rows.Close()

	var entries []roomdb.ListEntry
	for rows.Next() {
		var entry roomdb.ListEntry
		var pubKeyStr string
		if err := rows.Scan(&entry.ID, &pubKeyStr, &entry.Comment, &entry.AddedAt); err != nil {
			continue
		}
		ref, err := refs.ParseFeedRef(pubKeyStr)
		if err != nil {
			continue
		}
		entry.PubKey = *ref
		entries = append(entries, entry)
	}
	return entries, nil
}

func (d *DeniedKeys) Count(ctx context.Context) (uint, error) {
	var count uint
	err := d.db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM denied_keys").Scan(&count)
	return count, err
}

func (d *DeniedKeys) RemoveFeed(ctx context.Context, ref refs.FeedRef) error {
	_, err := d.db.conn.ExecContext(ctx,
		"DELETE FROM denied_keys WHERE pub_key = ?", ref.String())
	return err
}

func (d *DeniedKeys) RemoveID(ctx context.Context, id int64) error {
	_, err := d.db.conn.ExecContext(ctx,
		"DELETE FROM denied_keys WHERE id = ?", id)
	return err
}

type RoomConfig struct {
	db *DB
}

func (db *DB) RoomConfig() roomdb.RoomConfig {
	return &RoomConfig{db: db}
}

func (c *RoomConfig) GetPrivacyMode(ctx context.Context) (roomdb.PrivacyMode, error) {
	var value string
	err := c.db.conn.QueryRowContext(ctx,
		"SELECT value FROM room_config WHERE key = 'privacy_mode'").Scan(&value)
	if err != nil {
		return roomdb.ModeUnknown, fmt.Errorf("room config get privacy mode: %w", err)
	}
	return roomdb.ParsePrivacyMode(value), nil
}

func (c *RoomConfig) SetPrivacyMode(ctx context.Context, mode roomdb.PrivacyMode) error {
	var modeStr string
	switch mode {
	case roomdb.ModeOpen:
		modeStr = "open"
	case roomdb.ModeCommunity:
		modeStr = "community"
	case roomdb.ModeRestricted:
		modeStr = "restricted"
	default:
		return fmt.Errorf("invalid privacy mode: %d", mode)
	}
	_, err := c.db.conn.ExecContext(ctx,
		"INSERT OR REPLACE INTO room_config (key, value) VALUES ('privacy_mode', ?)", modeStr)
	return err
}

func (c *RoomConfig) GetDefaultLanguage(ctx context.Context) (string, error) {
	var value string
	err := c.db.conn.QueryRowContext(ctx,
		"SELECT value FROM room_config WHERE key = 'default_language'").Scan(&value)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("room config get default language: %w", err)
	}
	if err == sql.ErrNoRows {
		return "en", nil
	}
	return value, nil
}

func (c *RoomConfig) SetDefaultLanguage(ctx context.Context, lang string) error {
	_, err := c.db.conn.ExecContext(ctx,
		"INSERT OR REPLACE INTO room_config (key, value) VALUES ('default_language', ?)", lang)
	return err
}

type RuntimeSnapshots struct {
	db *DB
}

func (db *DB) RuntimeSnapshots() roomdb.RuntimeSnapshotsService {
	return &RuntimeSnapshots{db: db}
}

func (s *RuntimeSnapshots) MarkAllInactive(ctx context.Context) error {
	if _, err := s.db.conn.ExecContext(ctx, "UPDATE runtime_attendants SET active = FALSE"); err != nil {
		return fmt.Errorf("runtime snapshots mark attendants inactive: %w", err)
	}
	if _, err := s.db.conn.ExecContext(ctx, "UPDATE runtime_tunnel_endpoints SET active = FALSE"); err != nil {
		return fmt.Errorf("runtime snapshots mark tunnels inactive: %w", err)
	}
	return nil
}

func (s *RuntimeSnapshots) UpsertAttendant(ctx context.Context, id refs.FeedRef, addr string, connectedAt int64) error {
	connected := time.Unix(connectedAt, 0).UTC()
	if connectedAt <= 0 {
		connected = time.Now().UTC()
	}
	now := time.Now().UTC()
	_, err := s.db.conn.ExecContext(ctx, `
INSERT INTO runtime_attendants (feed_id, addr, connected_at, last_seen_at, active)
VALUES (?, ?, ?, ?, TRUE)
ON CONFLICT(feed_id) DO UPDATE SET
  addr = excluded.addr,
  connected_at = excluded.connected_at,
  last_seen_at = excluded.last_seen_at,
  active = TRUE
`, id.String(), addr, connected, now)
	if err != nil {
		return fmt.Errorf("runtime snapshots upsert attendant: %w", err)
	}
	return nil
}

func (s *RuntimeSnapshots) DeactivateAttendant(ctx context.Context, id refs.FeedRef) error {
	_, err := s.db.conn.ExecContext(ctx, `
UPDATE runtime_attendants
SET active = FALSE, last_seen_at = ?
WHERE feed_id = ?
`, time.Now().UTC(), id.String())
	if err != nil {
		return fmt.Errorf("runtime snapshots deactivate attendant: %w", err)
	}
	return nil
}

func (s *RuntimeSnapshots) ListAttendants(ctx context.Context, onlyActive bool) ([]roomdb.RuntimeAttendantSnapshot, error) {
	query := `
SELECT feed_id, addr, connected_at, last_seen_at, active
FROM runtime_attendants`
	if onlyActive {
		query += " WHERE active = TRUE"
	}
	query += " ORDER BY active DESC, last_seen_at DESC"
	rows, err := s.db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("runtime snapshots list attendants: %w", err)
	}
	defer rows.Close()

	out := make([]roomdb.RuntimeAttendantSnapshot, 0)
	for rows.Next() {
		var feedID string
		var addr string
		var connectedAt time.Time
		var lastSeenAt time.Time
		var active bool
		if err := rows.Scan(&feedID, &addr, &connectedAt, &lastSeenAt, &active); err != nil {
			return nil, fmt.Errorf("runtime snapshots scan attendant: %w", err)
		}
		ref, err := refs.ParseFeedRef(feedID)
		if err != nil {
			continue
		}
		out = append(out, roomdb.RuntimeAttendantSnapshot{
			ID:          *ref,
			Addr:        addr,
			ConnectedAt: connectedAt.Unix(),
			LastSeenAt:  lastSeenAt.Unix(),
			Active:      active,
		})
	}
	return out, nil
}

func (s *RuntimeSnapshots) UpsertTunnelEndpoint(ctx context.Context, target refs.FeedRef, addr string, announcedAt int64) error {
	announced := time.Unix(announcedAt, 0).UTC()
	if announcedAt <= 0 {
		announced = time.Now().UTC()
	}
	now := time.Now().UTC()
	_, err := s.db.conn.ExecContext(ctx, `
INSERT INTO runtime_tunnel_endpoints (target_feed, addr, announced_at, last_seen_at, active)
VALUES (?, ?, ?, ?, TRUE)
ON CONFLICT(target_feed) DO UPDATE SET
  addr = excluded.addr,
  announced_at = excluded.announced_at,
  last_seen_at = excluded.last_seen_at,
  active = TRUE
`, target.String(), addr, announced, now)
	if err != nil {
		return fmt.Errorf("runtime snapshots upsert tunnel endpoint: %w", err)
	}
	return nil
}

func (s *RuntimeSnapshots) DeactivateTunnelEndpoint(ctx context.Context, target refs.FeedRef) error {
	_, err := s.db.conn.ExecContext(ctx, `
UPDATE runtime_tunnel_endpoints
SET active = FALSE, last_seen_at = ?
WHERE target_feed = ?
`, time.Now().UTC(), target.String())
	if err != nil {
		return fmt.Errorf("runtime snapshots deactivate tunnel endpoint: %w", err)
	}
	return nil
}

func (s *RuntimeSnapshots) ListTunnelEndpoints(ctx context.Context, onlyActive bool) ([]roomdb.RuntimeTunnelEndpointSnapshot, error) {
	query := `
SELECT target_feed, addr, announced_at, last_seen_at, active
FROM runtime_tunnel_endpoints`
	if onlyActive {
		query += " WHERE active = TRUE"
	}
	query += " ORDER BY active DESC, last_seen_at DESC"
	rows, err := s.db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("runtime snapshots list tunnel endpoints: %w", err)
	}
	defer rows.Close()

	out := make([]roomdb.RuntimeTunnelEndpointSnapshot, 0)
	for rows.Next() {
		var targetFeed string
		var addr string
		var announcedAt time.Time
		var lastSeenAt time.Time
		var active bool
		if err := rows.Scan(&targetFeed, &addr, &announcedAt, &lastSeenAt, &active); err != nil {
			return nil, fmt.Errorf("runtime snapshots scan tunnel endpoint: %w", err)
		}
		ref, err := refs.ParseFeedRef(targetFeed)
		if err != nil {
			continue
		}
		out = append(out, roomdb.RuntimeTunnelEndpointSnapshot{
			Target:      *ref,
			Addr:        addr,
			AnnouncedAt: announcedAt.Unix(),
			LastSeenAt:  lastSeenAt.Unix(),
			Active:      active,
		})
	}
	return out, nil
}

type AuthTokens struct {
	db *DB
}

func (db *DB) AuthTokens() roomdb.AuthWithSSBService {
	return &AuthTokens{db: db}
}

func (a *AuthTokens) CreateToken(ctx context.Context, memberID int64) (string, error) {
	token := randomToken()
	_, err := a.db.conn.ExecContext(ctx,
		"INSERT INTO auth_tokens (token, member_id, created_at) VALUES (?, ?, ?)",
		token, memberID, time.Now())
	if err != nil {
		return "", fmt.Errorf("auth tokens create: %w", err)
	}
	return token, nil
}

func (a *AuthTokens) CheckToken(ctx context.Context, token string) (int64, error) {
	var memberID int64
	err := a.db.conn.QueryRowContext(ctx,
		"SELECT member_id FROM auth_tokens WHERE token = ?", token).Scan(&memberID)
	if err != nil {
		return 0, fmt.Errorf("auth tokens check: %w", err)
	}

	_, _ = a.db.conn.ExecContext(ctx,
		"UPDATE auth_tokens SET last_used_at = ? WHERE token = ?", time.Now(), token)

	return memberID, nil
}

func (a *AuthTokens) RemoveToken(ctx context.Context, token string) error {
	_, err := a.db.conn.ExecContext(ctx,
		"DELETE FROM auth_tokens WHERE token = ?", token)
	return err
}

func (a *AuthTokens) WipeTokensForMember(ctx context.Context, memberID int64) error {
	_, err := a.db.conn.ExecContext(ctx,
		"DELETE FROM auth_tokens WHERE member_id = ?", memberID)
	return err
}

func (a *AuthTokens) RotateToken(ctx context.Context, oldToken string) (string, error) {
	tx, err := a.db.conn.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("auth tokens rotate: begin: %w", err)
	}
	defer tx.Rollback()

	var memberID int64
	var rotationCount int
	err = tx.QueryRowContext(ctx,
		"SELECT member_id, rotation_count FROM auth_tokens WHERE token = ?", oldToken).Scan(&memberID, &rotationCount)
	if err != nil {
		return "", fmt.Errorf("auth tokens rotate: find old: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"DELETE FROM auth_tokens WHERE token = ?", oldToken)
	if err != nil {
		return "", fmt.Errorf("auth tokens rotate: delete old: %w", err)
	}

	newToken := randomToken()
	_, err = tx.ExecContext(ctx,
		"INSERT INTO auth_tokens (token, member_id, created_at, rotation_count) VALUES (?, ?, ?, ?)",
		newToken, memberID, time.Now(), rotationCount+1)
	if err != nil {
		return "", fmt.Errorf("auth tokens rotate: create new: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("auth tokens rotate: commit: %w", err)
	}

	return newToken, nil
}

func (a *AuthTokens) GetTokenInfo(ctx context.Context, token string) (roomdb.TokenInfo, error) {
	var info roomdb.TokenInfo
	var createdAt, lastUsedAt sql.NullTime
	err := a.db.conn.QueryRowContext(ctx,
		`SELECT member_id, created_at, last_used_at, rotation_count 
		 FROM auth_tokens WHERE token = ?`, token).Scan(
		&info.MemberID, &createdAt, &lastUsedAt, &info.RotationCount)
	if err != nil {
		return roomdb.TokenInfo{}, fmt.Errorf("auth tokens info: %w", err)
	}

	if createdAt.Valid {
		info.CreatedAt = createdAt.Time
	}
	if lastUsedAt.Valid {
		info.LastUsedAt = lastUsedAt.Time
	}

	return info, nil
}

type AuthFallback struct {
	db *DB
}

func (db *DB) AuthFallback() roomdb.AuthFallbackService {
	return &AuthFallback{db: db}
}

func (a *AuthFallback) Check(ctx context.Context, username, password string) (int64, error) {
	var storedHash []byte
	var memberID int64
	err := a.db.conn.QueryRowContext(ctx,
		"SELECT password_hash, member_id FROM fallback_passwords WHERE login = ?", username).
		Scan(&storedHash, &memberID)
	if err != nil {
		return 0, fmt.Errorf("auth fallback check: %w", err)
	}

	if !checkPasswordHash(password, storedHash) {
		return 0, fmt.Errorf("invalid credentials")
	}

	return memberID, nil
}

func (a *AuthFallback) SetPassword(ctx context.Context, memberID int64, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("auth fallback set password: %w", err)
	}

	var login string
	err = a.db.conn.QueryRowContext(ctx,
		"SELECT login FROM fallback_passwords WHERE member_id = ?", memberID).Scan(&login)
	if err == nil {
		_, err = a.db.conn.ExecContext(ctx,
			"UPDATE fallback_passwords SET password_hash = ? WHERE member_id = ?",
			hash, memberID)
	} else {
		login = fmt.Sprintf("member-%d", memberID)
		_, err = a.db.conn.ExecContext(ctx,
			"INSERT INTO fallback_passwords (login, password_hash, member_id) VALUES (?, ?, ?)",
			login, hash, memberID)
	}
	return err
}

func (a *AuthFallback) CreateResetToken(ctx context.Context, createdByMember, forMember int64) (string, error) {
	token := randomToken()
	_, err := a.db.conn.ExecContext(ctx,
		"INSERT INTO auth_tokens (token, member_id, created_at) VALUES (?, ?, ?)",
		token, forMember, time.Now())
	if err != nil {
		return "", fmt.Errorf("auth fallback create reset token: %w", err)
	}
	return token, nil
}

func (a *AuthFallback) SetPasswordWithToken(ctx context.Context, resetToken, password string) error {
	var memberID int64
	err := a.db.conn.QueryRowContext(ctx,
		"SELECT member_id FROM auth_tokens WHERE token = ?", resetToken).Scan(&memberID)
	if err != nil {
		return fmt.Errorf("auth fallback set password with token: %w", err)
	}

	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("auth fallback hash password: %w", err)
	}

	_, err = a.db.conn.ExecContext(ctx,
		"UPDATE fallback_passwords SET password_hash = ? WHERE member_id = ?",
		hash, memberID)
	if err != nil {
		return fmt.Errorf("auth fallback update password: %w", err)
	}

	_, _ = a.db.conn.ExecContext(ctx, "DELETE FROM auth_tokens WHERE token = ?", resetToken)
	return nil
}

func hashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

func checkPasswordHash(password string, hash []byte) bool {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	return err == nil
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(fmt.Errorf("failed to generate random token: %w", err))
	}
	return base64.URLEncoding.EncodeToString(b)
}
