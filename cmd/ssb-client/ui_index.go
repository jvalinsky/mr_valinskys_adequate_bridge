package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ssbcrypto "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/crypto"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	_ "modernc.org/sqlite"
)

type timelineMode string

const (
	timelineModeInbox    timelineMode = "inbox"
	timelineModeNetwork  timelineMode = "network"
	timelineModeProfile  timelineMode = "profile"
	timelineModeChannel  timelineMode = "channel"
	timelineModeMentions timelineMode = "mentions"
)

type timelineQuery struct {
	Mode     timelineMode
	Author   string
	Channel  string
	Search   string
	Limit    int
	Offset   int
	SelfFeed string
}

type indexedMessage struct {
	Key         string `json:"key"`
	Author      string `json:"author"`
	Sequence    int64  `json:"sequence"`
	TimestampMS int64  `json:"timestampMs"`
	ReceivedMS  int64  `json:"receivedMs"`
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Channel     string `json:"channel,omitempty"`
	Root        string `json:"root,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Fork        string `json:"fork,omitempty"`
	Contact     string `json:"contact,omitempty"`
	Following   bool   `json:"following,omitempty"`
	Blocking    bool   `json:"blocking,omitempty"`
	VoteLink    string `json:"voteLink,omitempty"`
	VoteValue   int64  `json:"voteValue,omitempty"`
	Recipient   string `json:"recipient,omitempty"`
	PrivatePeer string `json:"privatePeer,omitempty"`
	PrivateText string `json:"privateText,omitempty"`
	IsPrivate   bool   `json:"isPrivate"`
	RawJSON     string `json:"rawJson"`
}

type followerRelationship struct {
	Followers []string `json:"followers"`
	Following []string `json:"following"`
}

type channelSummary struct {
	Name     string `json:"name"`
	Count    int64  `json:"count"`
	LastPost int64  `json:"lastPostTs"`
}

type voteSummary struct {
	Target    string `json:"target"`
	VotesUp   int64  `json:"votesUp"`
	VotesDown int64  `json:"votesDown"`
	Total     int64  `json:"total"`
}

type conversationSummary struct {
	Peer         string `json:"peer"`
	LastTs       int64  `json:"lastTs"`
	MessageCount int64  `json:"messageCount"`
}

type uiIndex struct {
	mu sync.Mutex
	db *sql.DB
}

func newUIIndex(repoPath string) (*uiIndex, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, fmt.Errorf("repo path required")
	}
	dbPath := filepath.Join(repoPath, "ui-index.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=60000")
	if err != nil {
		return nil, fmt.Errorf("open ui index: %w", err)
	}
	idx := &uiIndex{db: db}
	if err := idx.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

func (idx *uiIndex) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.db == nil {
		return nil
	}
	return idx.db.Close()
}

func (idx *uiIndex) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS messages (
			msg_key TEXT PRIMARY KEY,
			author TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			timestamp_ms INTEGER NOT NULL,
			received_ms INTEGER NOT NULL,
			msg_type TEXT,
			text TEXT,
			channel TEXT,
			root TEXT,
			branch TEXT,
			fork TEXT,
			contact TEXT,
			following INTEGER NOT NULL DEFAULT 0,
			blocking INTEGER NOT NULL DEFAULT 0,
			vote_link TEXT,
			vote_value INTEGER,
			recipient TEXT,
			private_peer TEXT,
			private_text TEXT,
			is_private INTEGER NOT NULL DEFAULT 0,
			raw_json TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_author_seq ON messages(author, sequence DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_type ON messages(msg_type)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_root ON messages(root)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_branch ON messages(branch)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_private_peer ON messages(private_peer)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_vote_link ON messages(vote_link)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(timestamp_ms DESC)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			name TEXT PRIMARY KEY,
			seq INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sent_private_outbox (
			msg_key TEXT PRIMARY KEY,
			recipient TEXT NOT NULL,
			text TEXT NOT NULL,
			timestamp_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sent_private_recipient ON sent_private_outbox(recipient, timestamp_ms DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := idx.db.Exec(stmt); err != nil {
			return fmt.Errorf("init ui index schema: %w", err)
		}
	}
	return nil
}

func (idx *uiIndex) sync(store *feedlog.StoreImpl, whoami string, kp *keys.KeyPair) error {
	if idx == nil || idx.db == nil || store == nil {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if whoami != "" {
		if userLog, err := store.Logs().Get(whoami); err == nil {
			if err := idx.syncLog("user:"+whoami, userLog, whoami, kp); err != nil {
				return err
			}
		}
	}

	rxLog, err := store.ReceiveLog()
	if err == nil && rxLog != nil {
		if err := idx.syncLog("rx", rxLog, whoami, kp); err != nil {
			return err
		}
	}

	return nil
}

func (idx *uiIndex) syncLog(stateName string, log feedlog.Log, whoami string, kp *keys.KeyPair) error {
	if log == nil {
		return nil
	}
	current, err := log.Seq()
	if err != nil || current < 1 {
		return nil
	}

	lastSeq, err := idx.getSyncSeq(stateName)
	if err != nil {
		return err
	}

	if lastSeq >= current {
		return nil
	}

	for seq := lastSeq + 1; seq <= current; seq++ {
		msg, err := log.Get(seq)
		if err != nil {
			continue
		}
		rec := buildIndexedMessage(*msg, whoami, kp)
		if err := idx.upsertMessage(rec); err != nil {
			return err
		}
	}

	return idx.setSyncSeq(stateName, current)
}

func (idx *uiIndex) getSyncSeq(name string) (int64, error) {
	var seq int64
	err := idx.db.QueryRow(`SELECT seq FROM sync_state WHERE name = ?`, name).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get sync state %q: %w", name, err)
	}
	return seq, nil
}

func (idx *uiIndex) setSyncSeq(name string, seq int64) error {
	_, err := idx.db.Exec(`
		INSERT INTO sync_state(name, seq) VALUES(?, ?)
		ON CONFLICT(name) DO UPDATE SET seq = excluded.seq
	`, name, seq)
	if err != nil {
		return fmt.Errorf("set sync state %q: %w", name, err)
	}
	return nil
}

func (idx *uiIndex) upsertMessage(m indexedMessage) error {
	_, err := idx.db.Exec(`
		INSERT INTO messages(
			msg_key, author, sequence, timestamp_ms, received_ms, msg_type, text, channel,
			root, branch, fork, contact, following, blocking, vote_link, vote_value,
			recipient, private_peer, private_text, is_private, raw_json
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(msg_key) DO UPDATE SET
			author=excluded.author,
			sequence=excluded.sequence,
			timestamp_ms=excluded.timestamp_ms,
			received_ms=excluded.received_ms,
			msg_type=excluded.msg_type,
			text=excluded.text,
			channel=excluded.channel,
			root=excluded.root,
			branch=excluded.branch,
			fork=excluded.fork,
			contact=excluded.contact,
			following=excluded.following,
			blocking=excluded.blocking,
			vote_link=excluded.vote_link,
			vote_value=excluded.vote_value,
			recipient=excluded.recipient,
			private_peer=excluded.private_peer,
			private_text=excluded.private_text,
			is_private=excluded.is_private,
			raw_json=excluded.raw_json
	`,
		m.Key,
		m.Author,
		m.Sequence,
		m.TimestampMS,
		m.ReceivedMS,
		m.Type,
		m.Text,
		m.Channel,
		m.Root,
		m.Branch,
		m.Fork,
		m.Contact,
		boolToInt(m.Following),
		boolToInt(m.Blocking),
		m.VoteLink,
		nullInt64(m.VoteValue),
		m.Recipient,
		m.PrivatePeer,
		m.PrivateText,
		boolToInt(m.IsPrivate),
		m.RawJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert message %s: %w", m.Key, err)
	}
	return nil
}

func (idx *uiIndex) recordSentPrivate(msgKey, recipient, text string, ts int64) error {
	if idx == nil || idx.db == nil {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	_, err := idx.db.Exec(`
		INSERT INTO sent_private_outbox(msg_key, recipient, text, timestamp_ms)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(msg_key) DO UPDATE SET
			recipient=excluded.recipient,
			text=excluded.text,
			timestamp_ms=excluded.timestamp_ms
	`, msgKey, recipient, text, ts)
	if err != nil {
		return fmt.Errorf("record sent private message: %w", err)
	}
	return nil
}

func buildIndexedMessage(msg feedlog.StoredMessage, whoami string, kp *keys.KeyPair) indexedMessage {
	rec := indexedMessage{
		Key:         msg.Key,
		Author:      msg.Metadata.Author,
		Sequence:    msg.Metadata.Sequence,
		TimestampMS: msg.Metadata.Timestamp,
		ReceivedMS:  msg.Received,
		RawJSON:     string(msg.Value),
	}

	var content map[string]interface{}
	if err := json.Unmarshal(msg.Value, &content); err != nil {
		rec.Type = "raw"
		return rec
	}

	rec.Type = strings.TrimSpace(asString(content["type"]))
	rec.Text = strings.TrimSpace(asString(content["text"]))
	rec.Channel = strings.TrimSpace(asString(content["channel"]))
	rec.Root = firstRef(content["root"])
	rec.Branch = firstRef(content["branch"])
	rec.Fork = firstRef(content["fork"])

	if rec.Root == "" {
		rec.Root = strings.TrimSpace(msg.Metadata.Root)
	}
	if rec.Branch == "" && len(msg.Metadata.Parents) > 0 {
		rec.Branch = strings.TrimSpace(msg.Metadata.Parents[0])
	}

	if rec.Type == "contact" {
		rec.Contact = normalizeFeed(asString(content["contact"]))
		rec.Following = asBool(content["following"])
		rec.Blocking = asBool(content["blocking"])
	}

	if rec.Type == "vote" {
		if vote, ok := content["vote"].(map[string]interface{}); ok {
			rec.VoteLink = firstRef(vote["link"])
			rec.VoteValue = asInt64(vote["value"])
		}
	}

	rec.Recipient = normalizeFeed(asString(content["recipient"]))

	format := strings.TrimSpace(asString(content["format"]))
	if format == "box2" || format == "private-box" {
		rec.IsPrivate = true
		if rec.Type == "" {
			rec.Type = "private"
		}

		if kp != nil && format == "box2" {
			if peer, text, recipient, ok := tryDecryptBox2(msg, whoami, kp); ok {
				rec.PrivatePeer = peer
				rec.PrivateText = text
				if rec.Recipient == "" {
					rec.Recipient = recipient
				}
			}
		}
	}

	if rec.Type == "" {
		rec.Type = "unknown"
	}

	return rec
}

func tryDecryptBox2(msg feedlog.StoredMessage, whoami string, kp *keys.KeyPair) (peer string, text string, recipient string, ok bool) {
	if kp == nil || msg.Metadata == nil {
		return "", "", "", false
	}
	authorRef, err := refs.ParseFeedRef(msg.Metadata.Author)
	if err != nil {
		return "", "", "", false
	}
	authorPub := authorRef.PubKey()
	if len(authorPub) != 32 {
		return "", "", "", false
	}
	var senderPub [32]byte
	copy(senderPub[:], authorPub)

	recipientPub, recipientPriv := kp.ToCurve25519()
	plaintext, err := ssbcrypto.DecryptDM(msg.Value, recipientPub, recipientPriv, senderPub)
	if err != nil {
		return "", "", "", false
	}

	dm, err := ssbcrypto.UnwrapDMContent(plaintext)
	if err != nil {
		return "", "", "", false
	}

	recipient = normalizeFeed(dm.Recipient)
	text = dmContentToText(dm.Content)

	author := strings.TrimSpace(msg.Metadata.Author)
	self := strings.TrimSpace(whoami)
	switch {
	case author == self && recipient != "":
		peer = recipient
	case recipient == self:
		peer = author
	default:
		peer = ""
	}

	return peer, text, recipient, peer != ""
}

func dmContentToText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]interface{}:
		if text := strings.TrimSpace(asString(v["text"])); text != "" {
			return text
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func (idx *uiIndex) queryTimeline(q timelineQuery) ([]indexedMessage, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	args := []interface{}{}
	where := []string{"1=1"}

	if strings.TrimSpace(q.Search) != "" {
		where = append(where, "(text LIKE ? OR raw_json LIKE ? OR author LIKE ?)")
		needle := "%" + q.Search + "%"
		args = append(args, needle, needle, needle)
	}

	switch q.Mode {
	case timelineModeProfile:
		author := normalizeFeed(q.Author)
		if author != "" {
			where = append(where, "author = ?")
			args = append(args, author)
		}
	case timelineModeChannel:
		channel := strings.TrimSpace(q.Channel)
		if channel != "" {
			where = append(where, "channel = ?")
			args = append(args, channel)
		}
	case timelineModeMentions:
		if q.SelfFeed != "" {
			where = append(where, "raw_json LIKE ?")
			args = append(args, "%"+q.SelfFeed+"%")
		}
	case timelineModeInbox:
		followed, err := idx.followingSet(q.SelfFeed)
		if err != nil {
			return nil, err
		}
		if len(followed) == 0 {
			where = append(where, "author = ?")
			args = append(args, q.SelfFeed)
		} else {
			ids := []string{q.SelfFeed}
			for feed := range followed {
				ids = append(ids, feed)
			}
			sort.Strings(ids)
			where = append(where, "author IN ("+placeholders(len(ids))+")")
			for _, id := range ids {
				args = append(args, id)
			}
		}
	case timelineModeNetwork:
		fallthrough
	default:
	}

	query := `
		SELECT
			msg_key, author, sequence, timestamp_ms, received_ms, msg_type, text, channel,
			root, branch, fork, contact, following, blocking, vote_link, vote_value,
			recipient, private_peer, private_text, is_private, raw_json
		FROM messages
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY timestamp_ms DESC, sequence DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, q.Limit, q.Offset)
	return idx.queryMessages(query, args...)
}

func (idx *uiIndex) queryMessages(query string, args ...interface{}) ([]indexedMessage, error) {
	rows, err := idx.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []indexedMessage
	for rows.Next() {
		var m indexedMessage
		var following, blocking, isPrivate int
		var voteValue sql.NullInt64
		if err := rows.Scan(
			&m.Key,
			&m.Author,
			&m.Sequence,
			&m.TimestampMS,
			&m.ReceivedMS,
			&m.Type,
			&m.Text,
			&m.Channel,
			&m.Root,
			&m.Branch,
			&m.Fork,
			&m.Contact,
			&following,
			&blocking,
			&m.VoteLink,
			&voteValue,
			&m.Recipient,
			&m.PrivatePeer,
			&m.PrivateText,
			&isPrivate,
			&m.RawJSON,
		); err != nil {
			return nil, err
		}
		m.Following = following == 1
		m.Blocking = blocking == 1
		m.IsPrivate = isPrivate == 1
		if voteValue.Valid {
			m.VoteValue = voteValue.Int64
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (idx *uiIndex) followingSet(whoami string) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	if strings.TrimSpace(whoami) == "" {
		return result, nil
	}

	rows, err := idx.db.Query(`
		SELECT contact, following, blocking
		FROM (
			SELECT
				contact,
				following,
				blocking,
				ROW_NUMBER() OVER (PARTITION BY contact ORDER BY sequence DESC) AS rn
			FROM messages
			WHERE msg_type = 'contact' AND author = ? AND contact != ''
		)
		WHERE rn = 1
	`, whoami)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var contact string
		var following, blocking int
		if err := rows.Scan(&contact, &following, &blocking); err != nil {
			return nil, err
		}
		if following == 1 && blocking == 0 {
			result[contact] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (idx *uiIndex) queryThread(msgKey string) ([]indexedMessage, string, error) {
	msgKey = strings.TrimSpace(msgKey)
	if msgKey == "" {
		return nil, "", nil
	}

	var root string
	err := idx.db.QueryRow(`
		SELECT CASE WHEN root != '' THEN root ELSE msg_key END
		FROM messages
		WHERE msg_key = ?
	`, msgKey).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}

	msgs, err := idx.queryMessages(`
		SELECT
			msg_key, author, sequence, timestamp_ms, received_ms, msg_type, text, channel,
			root, branch, fork, contact, following, blocking, vote_link, vote_value,
			recipient, private_peer, private_text, is_private, raw_json
		FROM messages
		WHERE msg_key = ? OR root = ? OR branch = ? OR branch = ? OR fork = ?
		ORDER BY timestamp_ms ASC, sequence ASC
	`, root, root, root, msgKey, root)
	if err != nil {
		return nil, "", err
	}
	return msgs, root, nil
}

func (idx *uiIndex) queryChannels(limit int) ([]channelSummary, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := idx.db.Query(`
		SELECT channel, COUNT(*) AS cnt, MAX(timestamp_ms) AS last_ts
		FROM messages
		WHERE channel != ''
		GROUP BY channel
		ORDER BY cnt DESC, channel ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]channelSummary, 0, limit)
	for rows.Next() {
		var item channelSummary
		if err := rows.Scan(&item.Name, &item.Count, &item.LastPost); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (idx *uiIndex) queryVotes(target string, limit int) ([]voteSummary, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []interface{}{}
	where := "msg_type = 'vote' AND vote_link != ''"
	if strings.TrimSpace(target) != "" {
		where += " AND vote_link = ?"
		args = append(args, target)
	}
	query := `
		SELECT
			vote_link,
			SUM(CASE WHEN vote_value > 0 THEN 1 ELSE 0 END) AS up_votes,
			SUM(CASE WHEN vote_value < 0 THEN 1 ELSE 0 END) AS down_votes,
			COUNT(*) AS total_votes
		FROM messages
		WHERE ` + where + `
		GROUP BY vote_link
		ORDER BY total_votes DESC, vote_link ASC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := idx.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []voteSummary{}
	for rows.Next() {
		var item voteSummary
		if err := rows.Scan(&item.Target, &item.VotesUp, &item.VotesDown, &item.Total); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (idx *uiIndex) queryFollowers(whoami string) (followerRelationship, error) {
	res := followerRelationship{}
	if strings.TrimSpace(whoami) == "" {
		return res, nil
	}

	rows, err := idx.db.Query(`
		SELECT author, contact, following, blocking
		FROM (
			SELECT
				author,
				contact,
				following,
				blocking,
				ROW_NUMBER() OVER (PARTITION BY author, contact ORDER BY sequence DESC) AS rn
			FROM messages
			WHERE msg_type = 'contact' AND contact != ''
		)
		WHERE rn = 1
	`)
	if err != nil {
		return res, err
	}
	defer rows.Close()

	followers := map[string]struct{}{}
	following := map[string]struct{}{}
	for rows.Next() {
		var author, contact string
		var isFollowing, isBlocking int
		if err := rows.Scan(&author, &contact, &isFollowing, &isBlocking); err != nil {
			return res, err
		}
		if isFollowing == 1 && isBlocking == 0 {
			if author == whoami {
				following[contact] = struct{}{}
			}
			if contact == whoami {
				followers[author] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}

	res.Followers = mapKeys(followers)
	res.Following = mapKeys(following)
	return res, nil
}

func (idx *uiIndex) queryConversations() ([]conversationSummary, error) {
	rows, err := idx.db.Query(`
		SELECT peer, MAX(ts) AS last_ts, SUM(cnt) AS total_count
		FROM (
			SELECT private_peer AS peer, MAX(timestamp_ms) AS ts, COUNT(*) AS cnt
			FROM messages
			WHERE private_peer != ''
			GROUP BY private_peer
			UNION ALL
			SELECT recipient AS peer, MAX(timestamp_ms) AS ts, COUNT(*) AS cnt
			FROM sent_private_outbox
			WHERE recipient != ''
			GROUP BY recipient
		)
		GROUP BY peer
		ORDER BY last_ts DESC, peer ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []conversationSummary
	for rows.Next() {
		var item conversationSummary
		if err := rows.Scan(&item.Peer, &item.LastTs, &item.MessageCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (idx *uiIndex) queryConversationMessages(peer string, limit int) ([]map[string]interface{}, error) {
	peer = normalizeFeed(peer)
	if peer == "" {
		return []map[string]interface{}{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := idx.db.Query(`
		SELECT author, timestamp_ms, private_text, msg_key, 'inbox' AS source
		FROM messages
		WHERE private_peer = ?
		UNION ALL
		SELECT ? AS author, timestamp_ms, text, msg_key, 'outbox' AS source
		FROM sent_private_outbox
		WHERE recipient = ?
		ORDER BY timestamp_ms ASC
		LIMIT ?
	`, peer, peer, peer, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]interface{}{}
	for rows.Next() {
		var author, text, key, source string
		var ts int64
		if err := rows.Scan(&author, &ts, &text, &key, &source); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"author":      author,
			"timestampMs": ts,
			"text":        text,
			"key":         key,
			"source":      source,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func asBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		t = strings.TrimSpace(strings.ToLower(t))
		return t == "1" || t == "true" || t == "yes" || t == "on"
	case float64:
		return t != 0
	case int64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}

func asInt64(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i
	default:
		return 0
	}
}

func firstRef(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []interface{}:
		for _, item := range t {
			if s := firstRef(item); s != "" {
				return s
			}
		}
	case map[string]interface{}:
		if link := strings.TrimSpace(asString(t["link"])); link != "" {
			return link
		}
	}
	return ""
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullInt64(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func normalizeFeed(feed string) string {
	feed = strings.TrimSpace(feed)
	if feed == "" {
		return ""
	}
	if !strings.HasPrefix(feed, "@") {
		feed = "@" + feed
	}
	return feed
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	ph := make([]string, n)
	for i := 0; i < n; i++ {
		ph[i] = "?"
	}
	return strings.Join(ph, ",")
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}
