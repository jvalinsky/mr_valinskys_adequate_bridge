package feedlog

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/tangle"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

var (
	ErrInvalidMessage = errors.New("feedlog: invalid message")
	ErrSeqNotFound    = errors.New("feedlog: sequence not found")
	ErrFeedNotFound   = errors.New("feedlog: feed not found")
	ErrInvalidContent = errors.New("feedlog: invalid content")
	ErrNotFound       = errors.New("sqlite: not found")
)

const (
	sqliteBusyRetryLimit = 20
	sqliteBusyRetryDelay = 25 * time.Millisecond
)

type Log interface {
	Seq() (int64, error)
	Append(content []byte, metadata *Metadata) (int64, error)
	Get(seq int64) (*StoredMessage, error)
	Query(specs ...QuerySpec) (Source, error)
	Close() error
}

type StoredMessage struct {
	Key      string
	Value    []byte
	Metadata *Metadata
	Received int64
}

type Metadata struct {
	Author     string
	Sequence   int64
	Previous   string
	Timestamp  int64
	Sig        []byte
	Hash       string
	TangleName string
	Root       string
	Parents    []string
}

type QuerySpec interface{}

type Source interface {
	Next(ctx context.Context) (*StoredMessage, error)
	Close() error
}

type MultiLog interface {
	List() ([]string, error)
	Get(author string) (Log, error)
	Create(author string) (Log, error)
	Has(author string) (bool, error)
	Close() error
}

type FeedStore interface {
	Logs() MultiLog
	ReceiveLog() (Log, error)
	Blobs() BlobStore
	Tangles() TangleStorage
	Close() error
}

type TangleStorage interface {
	AddMessage(ctx context.Context, msgKey, tangleName, rootKey string, parentKeys []string) error
	GetTangle(ctx context.Context, name, root string) (*tangle.Tangle, error)
	GetTangleMessages(ctx context.Context, name, root string, sinceSeq int64) ([]tangle.MessageWithTangles, error)
	GetMessagesByParent(ctx context.Context, parentKey string) ([]tangle.MessageWithTangles, error)
	GetTangleTips(ctx context.Context, name, root string) ([]string, error)
	GetTangleMembership(ctx context.Context, msgKey string) (*tangle.TangleMembership, error)
	GetTangleMessageCount(ctx context.Context, name, root string) (int, error)
	Close() error
}

type BlobStore interface {
	Put(r io.Reader) ([]byte, error)
	Get(hash []byte) (io.ReadCloser, error)
	GetRange(hash []byte, start, size int64) (io.ReadCloser, error)
	Has(hash []byte) (bool, error)
	Size(hash []byte) (int64, error)
	Delete(hash []byte) error
	Close() error
}

type FeedLog struct {
	store FeedStore
	feed  string
	log   Log
}

func NewFeedLog(store FeedStore, feed string) (*FeedLog, error) {
	log, err := store.Logs().Get(feed)
	if err == ErrNotFound {
		return nil, ErrFeedNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("feedlog: failed to get log: %w", err)
	}

	return &FeedLog{
		store: store,
		feed:  feed,
		log:   log,
	}, nil
}

func CreateFeedLog(store FeedStore, feed string) (*FeedLog, error) {
	log, err := store.Logs().Create(feed)
	if err != nil {
		return nil, fmt.Errorf("feedlog: failed to create log: %w", err)
	}

	return &FeedLog{
		store: store,
		feed:  feed,
		log:   log,
	}, nil
}

func (f *FeedLog) Seq() (int64, error) {
	return f.log.Seq()
}

func (f *FeedLog) Append(content []byte, metadata *Metadata) (int64, error) {
	return f.log.Append(content, metadata)
}

func (f *FeedLog) Get(seq int64) (*StoredMessage, error) {
	return f.log.Get(seq)
}

func (f *FeedLog) Query(specs ...QuerySpec) (Source, error) {
	return nil, errors.New("not implemented")
}

func (f *FeedLog) Close() error {
	return f.log.Close()
}

type storedMessageWrapper struct {
	Content  []byte    `json:"content"`
	Metadata *Metadata `json:"metadata"`
}

type StoreImpl struct {
	db       *sql.DB
	blobPath string
	mu       sync.RWMutex
	rxLog    *receiveLog
}

type Config struct {
	DBPath     string
	RepoPath   string
	BlobSubdir string
}

func NewStore(cfg Config) (*StoreImpl, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.DBPath+"?_journal_mode=WAL&_busy_timeout=60000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	blobPath := cfg.RepoPath
	if blobPath != "" && cfg.BlobSubdir != "" {
		blobPath = filepath.Join(blobPath, cfg.BlobSubdir)
		if err := os.MkdirAll(blobPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create blob directory: %w", err)
		}
	}

	s := &StoreImpl{
		db:       db,
		blobPath: blobPath,
		rxLog:    &receiveLog{db: db},
	}

	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return s, nil
}

func (s *StoreImpl) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS feeds (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			addr BLOB UNIQUE NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_feeds_addr ON feeds(addr)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			feed_id INTEGER NOT NULL,
			seq INTEGER NOT NULL,
			key TEXT NOT NULL,
			value_json BLOB NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (feed_id) REFERENCES feeds(id),
			UNIQUE(feed_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_feed_seq ON messages(feed_id, seq)`,
		`CREATE TABLE IF NOT EXISTS messages_key_idx (
			key TEXT UNIQUE NOT NULL,
			feed_id INTEGER NOT NULL,
			seq INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_key ON messages_key_idx(key)`,
		`CREATE TABLE IF NOT EXISTS receive_log (
			id INTEGER PRIMARY KEY,
			seq INTEGER NOT NULL,
			key TEXT NOT NULL,
			value_json BLOB NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS blobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hash BLOB UNIQUE NOT NULL,
			size INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_blobs_hash ON blobs(hash)`,
		`CREATE TABLE IF NOT EXISTS tangles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			root TEXT NOT NULL,
			tips TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(name, root)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tangles_name_root ON tangles(name, root)`,
		`CREATE TABLE IF NOT EXISTS tangle_membership (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_key TEXT NOT NULL,
			tangle_name TEXT NOT NULL,
			root_key TEXT NOT NULL,
			parent_keys TEXT,
			created_at INTEGER NOT NULL,
			UNIQUE(message_key, tangle_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tangle_membership_tangle ON tangle_membership(tangle_name, root_key)`,
		`CREATE INDEX IF NOT EXISTS idx_tangle_membership_root ON tangle_membership(root_key)`,
	}

	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	)`)
	if err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	var version int
	row := s.db.QueryRow("SELECT version FROM schema_version LIMIT 1")
	if err := row.Scan(&version); err == sql.ErrNoRows {
		version = 0
	} else if err != nil {
		return err
	}

	for i, sql := range migrations[version:] {
		if _, err := s.db.Exec(sql); err != nil {
			return fmt.Errorf("migration %d failed: %w", version+i, err)
		}
		if _, err := s.db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", version+i+1); err != nil {
			return err
		}
	}

	return nil
}

func (s *StoreImpl) Logs() MultiLog {
	ml := &multiLog{db: s.db}
	ml.SetTangles(s.Tangles())
	return ml
}

func (s *StoreImpl) ReceiveLog() (Log, error) {
	return s.rxLog, nil
}

func (s *StoreImpl) SetMessageLogger(logger MessageLogger) {
	if s.rxLog != nil {
		s.rxLog.SetLogger(logger)
	}
}

func (s *StoreImpl) SetSignatureLogger(logger SignatureLogger) {
	if s.rxLog != nil {
		s.rxLog.SetSignatureLogger(logger)
	}
}

func (s *StoreImpl) SetSignatureVerifier(verifier SignatureVerifier) {
	if s.rxLog != nil {
		s.rxLog.SetSignatureVerifier(verifier)
	}
}

func (s *StoreImpl) SetDMHandler(handler DMHandler) {
	if s.rxLog != nil {
		s.rxLog.SetDMHandler(handler)
	}
}

func (s *StoreImpl) Blobs() BlobStore {
	return &blobStore{db: s.db, blobPath: s.blobPath}
}

func (s *StoreImpl) Tangles() TangleStorage {
	return &tangleStore{db: s.db}
}

type tangleStore struct {
	db *sql.DB
}

func (ts *tangleStore) AddMessage(ctx context.Context, msgKey, tangleName, rootKey string, parentKeys []string) error {
	parentJSON, _ := json.Marshal(parentKeys)
	_, err := ts.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO tangle_membership (message_key, tangle_name, root_key, parent_keys, created_at) VALUES (?, ?, ?, ?, ?)`,
		msgKey, tangleName, rootKey, string(parentJSON), time.Now().Unix())
	return err
}

func (ts *tangleStore) GetTangle(ctx context.Context, name, root string) (*tangle.Tangle, error) {
	row := ts.db.QueryRowContext(ctx,
		`SELECT id, name, root, tips, created_at, updated_at FROM tangles WHERE name = ? AND root = ?`,
		name, root)

	var t tangle.Tangle
	var tipsJSON string
	err := row.Scan(&t.ID, &t.Name, &t.Root, &tipsJSON, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tipsJSON), &t.Tips)
	return &t, nil
}

func (ts *tangleStore) GetTangleMessages(ctx context.Context, name, root string, sinceSeq int64) ([]tangle.MessageWithTangles, error) {
	rows, err := ts.db.QueryContext(ctx,
		`SELECT tm.message_key, tm.tangle_name, tm.root_key, tm.parent_keys, m.value_json
		 FROM tangle_membership tm
		 LEFT JOIN messages m ON m.key = tm.message_key
		 WHERE tm.tangle_name = ? AND tm.root_key = ?
		 ORDER BY m.seq ASC`,
		name, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []tangle.MessageWithTangles
	for rows.Next() {
		var m tangle.MessageWithTangles
		var parentJSON string
		var content []byte
		err := rows.Scan(&m.Key, &m.TangleName, &m.Root, &parentJSON, &content)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(parentJSON), &m.Parents)
		m.Content = content
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (ts *tangleStore) GetMessagesByParent(ctx context.Context, parentKey string) ([]tangle.MessageWithTangles, error) {
	rows, err := ts.db.QueryContext(ctx,
		`SELECT message_key, tangle_name, root_key, parent_keys FROM tangle_membership WHERE parent_keys LIKE ?`,
		"%"+parentKey+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []tangle.MessageWithTangles
	for rows.Next() {
		var m tangle.MessageWithTangles
		var parentJSON string
		err := rows.Scan(&m.Key, &m.TangleName, &m.Root, &parentJSON)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(parentJSON), &m.Parents)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (ts *tangleStore) GetTangleTips(ctx context.Context, name, root string) ([]string, error) {
	rows, err := ts.db.QueryContext(ctx,
		`SELECT tm.message_key FROM tangle_membership tm
		 WHERE tm.tangle_name = ? AND tm.root_key = ?
		 AND NOT EXISTS (
		   SELECT 1 FROM tangle_membership tm2
		   WHERE tm2.parent_keys LIKE '%' || tm.message_key || '%'
		 )`,
		name, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tips []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			tips = append(tips, key)
		}
	}
	return tips, nil
}

func (ts *tangleStore) GetTangleMembership(ctx context.Context, msgKey string) (*tangle.TangleMembership, error) {
	row := ts.db.QueryRowContext(ctx,
		`SELECT id, message_key, tangle_name, root_key, parent_keys, created_at FROM tangle_membership WHERE message_key = ?`,
		msgKey)

	var tm tangle.TangleMembership
	var parentJSON string
	err := row.Scan(&tm.ID, &tm.MessageKey, &tm.TangleName, &tm.RootKey, &parentJSON, &tm.CreatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(parentJSON), &tm.ParentKeys)
	return &tm, nil
}

func (ts *tangleStore) GetTangleMessageCount(ctx context.Context, name, root string) (int, error) {
	var count int
	row := ts.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tangle_membership WHERE tangle_name = ? AND root_key = ?`,
		name, root)
	err := row.Scan(&count)
	return count, err
}

func (ts *tangleStore) Close() error {
	return nil
}

func (s *StoreImpl) Close() error {
	return s.db.Close()
}

type multiLog struct {
	db      *sql.DB
	mu      sync.RWMutex
	tangles TangleStorage
}

func (m *multiLog) SetTangles(ts TangleStorage) {
	m.tangles = ts
}

func (m *multiLog) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query("SELECT addr FROM feeds ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addrs []string
	for rows.Next() {
		var addr []byte
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		addrs = append(addrs, string(addr))
	}
	return addrs, rows.Err()
}

func (m *multiLog) Get(addr string) (Log, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var feedID int64
	for attempt := 0; ; attempt++ {
		err := m.db.QueryRow("SELECT id FROM feeds WHERE addr = ?", []byte(addr)).Scan(&feedID)
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		if err != nil {
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return nil, err
		}
		break
	}

	la := &logAdapter{db: m.db, feedID: feedID, tangles: m.tangles}
	return la, nil
}

func (m *multiLog) Create(addr string) (Log, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var id int64
	for attempt := 0; ; attempt++ {
		if _, err := m.db.Exec("INSERT OR IGNORE INTO feeds (addr, created_at) VALUES (?, ?)", []byte(addr), now()); err != nil {
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return nil, err
		}

		err := m.db.QueryRow("SELECT id FROM feeds WHERE addr = ?", []byte(addr)).Scan(&id)
		if err != nil {
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return nil, err
		}
		break
	}

	la := &logAdapter{db: m.db, feedID: id, tangles: m.tangles}
	return la, nil
}

func (m *multiLog) Has(addr string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var exists int
	for attempt := 0; ; attempt++ {
		err := m.db.QueryRow("SELECT 1 FROM feeds WHERE addr = ?", []byte(addr)).Scan(&exists)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return false, err
		}
		break
	}
	return true, nil
}

func (m *multiLog) Close() error {
	return nil
}

type logAdapter struct {
	db      *sql.DB
	feedID  int64
	mu      sync.RWMutex
	tangles TangleStorage
}

func (l *logAdapter) SetTangles(ts TangleStorage) {
	l.tangles = ts
}

func (l *logAdapter) Seq() (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var seq sql.NullInt64
	err := l.db.QueryRow("SELECT MAX(seq) FROM messages WHERE feed_id = ?", l.feedID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return -1, nil
	}
	return seq.Int64, nil
}

func (l *logAdapter) Append(content []byte, metadata *Metadata) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	wrapper := &storedMessageWrapper{
		Content:  content,
		Metadata: metadata,
	}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return 0, err
	}

	var (
		nextSeq int64
		key     string
	)
	for attempt := 0; ; attempt++ {
		tx, err := l.db.BeginTx(context.Background(), nil)
		if err != nil {
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		var currentSeq sql.NullInt64
		if err := tx.QueryRow("SELECT MAX(seq) FROM messages WHERE feed_id = ?", l.feedID).Scan(&currentSeq); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		nextSeq = int64(1)
		if currentSeq.Valid {
			nextSeq = currentSeq.Int64 + 1
		}
		key = fmt.Sprintf("%x", sha256.Sum256(data))

		if _, err := tx.Exec(
			"INSERT INTO messages (feed_id, seq, key, value_json, created_at) VALUES (?, ?, ?, ?, ?)",
			l.feedID, nextSeq, key, data, now(),
		); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO messages_key_idx (key, feed_id, seq) VALUES (?, ?, ?)",
			key, l.feedID, nextSeq,
		); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}
		break
	}

	if l.tangles != nil && metadata != nil && metadata.TangleName != "" {
		_ = l.tangles.AddMessage(context.Background(), key, metadata.TangleName, metadata.Root, metadata.Parents)
	}

	return nextSeq, nil
}

func (l *logAdapter) Get(seq int64) (*StoredMessage, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var data []byte
	var createdAt int64
	err := l.db.QueryRow("SELECT value_json, created_at FROM messages WHERE feed_id = ? AND seq = ?", l.feedID, seq).Scan(&data, &createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var wrapper storedMessageWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}

	return &StoredMessage{
		Key:      wrapper.Metadata.Hash,
		Value:    wrapper.Content,
		Metadata: wrapper.Metadata,
		Received: createdAt,
	}, nil
}

func (l *logAdapter) Query(specs ...QuerySpec) (Source, error) {
	return nil, errors.New("not implemented")
}

func (l *logAdapter) Close() error {
	return nil
}

type MessageLogger func(author string, seq int64, msgType string, key string)

type SignatureLogger func(author string, seq int64, key string, valid bool, err error)

type DMHandler func(senderFeed, recipientFeed, ciphertext, plaintext string) error

type receiveLog struct {
	db          *sql.DB
	mu          sync.RWMutex
	logger      MessageLogger
	sigLogger   SignatureLogger
	sigVerifier SignatureVerifier
	dmHandler   DMHandler
}

type SignatureVerifier interface {
	Verify(content []byte, metadata *Metadata) error
}

type DefaultSignatureVerifier struct{}

func (v *DefaultSignatureVerifier) Verify(content []byte, metadata *Metadata) error {
	if len(metadata.Sig) == 0 {
		return errors.New("missing signature")
	}

	if len(metadata.Sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature length: %d (expected %d)", len(metadata.Sig), ed25519.SignatureSize)
	}

	msg, err := legacy.VerifySignedMessageJSON(content)
	if err != nil {
		return fmt.Errorf("parse signed message: %w", err)
	}

	msgRef, err := legacy.SignedMessageRefFromJSON(content)
	if err != nil {
		return fmt.Errorf("derive message ref: %w", err)
	}

	if metadata.Author != "" && metadata.Author != msg.Author.String() {
		return fmt.Errorf("author mismatch: metadata=%s message=%s", metadata.Author, msg.Author.String())
	}
	if metadata.Sequence != 0 && metadata.Sequence != msg.Sequence {
		return fmt.Errorf("sequence mismatch: metadata=%d message=%d", metadata.Sequence, msg.Sequence)
	}
	if len(metadata.Sig) > 0 && !bytes.Equal(metadata.Sig, msg.Signature) {
		return errors.New("signature mismatch")
	}
	if metadata.Hash != "" && metadata.Hash != msgRef.String() {
		return fmt.Errorf("message ref mismatch: metadata=%s message=%s", metadata.Hash, msgRef.String())
	}

	return nil
}

func (l *receiveLog) SetSignatureLogger(logger SignatureLogger) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sigLogger = logger
}

func (l *receiveLog) SetSignatureVerifier(verifier SignatureVerifier) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if verifier == nil {
		l.sigVerifier = &DefaultSignatureVerifier{}
	} else {
		l.sigVerifier = verifier
	}
}

func (l *receiveLog) SetLogger(logger MessageLogger) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger = logger
}

func (l *receiveLog) SetDMHandler(handler DMHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dmHandler = handler
}

func (l *receiveLog) Seq() (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var seq sql.NullInt64
	err := l.db.QueryRow("SELECT MAX(seq) FROM receive_log").Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return -1, nil
	}
	return seq.Int64, nil
}

func (l *receiveLog) Append(content []byte, metadata *Metadata) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.sigVerifier != nil && metadata != nil {
		if err := l.sigVerifier.Verify(content, metadata); err != nil {
			if l.sigLogger != nil {
				go l.sigLogger(metadata.Author, metadata.Sequence, "", false, err)
			}
			return 0, fmt.Errorf("signature verification failed: %w", err)
		}
		if l.sigLogger != nil {
			go l.sigLogger(metadata.Author, metadata.Sequence, metadata.Hash, true, nil)
		}
	}

	wrapper := &storedMessageWrapper{
		Content:  content,
		Metadata: metadata,
	}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return 0, err
	}

	var (
		nextSeq int64
		key     string
	)
	for attempt := 0; ; attempt++ {
		tx, err := l.db.BeginTx(context.Background(), nil)
		if err != nil {
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		var currentSeq sql.NullInt64
		if err := tx.QueryRow("SELECT MAX(seq) FROM receive_log").Scan(&currentSeq); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		nextSeq = int64(1)
		if currentSeq.Valid {
			nextSeq = currentSeq.Int64 + 1
		}

		key = fmt.Sprintf("%x", sha256.Sum256(data))
		if _, err := tx.Exec(
			"INSERT INTO receive_log (seq, key, value_json, created_at) VALUES (?, ?, ?, ?)",
			nextSeq, key, data, now(),
		); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			if shouldRetrySQLiteBusy(err, attempt) {
				continue
			}
			return 0, err
		}
		break
	}

	if l.logger != nil && metadata != nil {
		var msgType string
		if wrapper, ok := parseMessageType(content); ok {
			msgType = wrapper.Type
		}
		go l.logger(metadata.Author, nextSeq, msgType, key)
	}

	return nextSeq, nil
}

func (l *receiveLog) Get(seq int64) (*StoredMessage, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var data []byte
	var createdAt int64
	err := l.db.QueryRow("SELECT value_json, created_at FROM receive_log WHERE seq = ?", seq).Scan(&data, &createdAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var wrapper storedMessageWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}

	return &StoredMessage{
		Key:      wrapper.Metadata.Hash,
		Value:    wrapper.Content,
		Metadata: wrapper.Metadata,
		Received: createdAt,
	}, nil
}

func (l *receiveLog) Query(specs ...QuerySpec) (Source, error) {
	return nil, errors.New("not implemented")
}

func (l *receiveLog) Close() error {
	return nil
}

func shouldRetrySQLiteBusy(err error, attempt int) bool {
	if err == nil || attempt >= sqliteBusyRetryLimit-1 {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY") {
		time.Sleep(sqliteBusyRetryDelay)
		return true
	}
	return false
}

type blobStore struct {
	db       *sql.DB
	blobPath string
	mu       sync.RWMutex
}

func (b *blobStore) Put(r io.Reader) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	hash := sha256Hash(data)

	if b.blobPath != "" {
		blobFile := filepath.Join(b.blobPath, fmt.Sprintf("%x", hash))
		if err := os.MkdirAll(filepath.Dir(blobFile), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(blobFile, data, 0644); err != nil {
			return nil, err
		}
	}

	_, err = b.db.Exec(
		"INSERT OR REPLACE INTO blobs (hash, size, created_at) VALUES (?, ?, ?)",
		hash, int64(len(data)), now(),
	)
	if err != nil {
		return nil, err
	}

	return hash, nil
}

func (b *blobStore) Get(hash []byte) (io.ReadCloser, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var size int64
	err := b.db.QueryRow("SELECT size FROM blobs WHERE hash = ?", hash).Scan(&size)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if b.blobPath != "" {
		blobFile := filepath.Join(b.blobPath, fmt.Sprintf("%x", hash))
		return os.Open(blobFile)
	}

	return io.NopCloser(io.LimitReader(nil, size)), nil
}

func (b *blobStore) GetRange(hash []byte, start, size int64) (io.ReadCloser, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var totalSize int64
	err := b.db.QueryRow("SELECT size FROM blobs WHERE hash = ?", hash).Scan(&totalSize)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if b.blobPath != "" {
		blobFile := filepath.Join(b.blobPath, fmt.Sprintf("%x", hash))
		f, err := os.Open(blobFile)
		if err != nil {
			return nil, err
		}
		_, err = f.Seek(start, io.SeekStart)
		if err != nil {
			f.Close()
			return nil, err
		}
		return io.NopCloser(io.LimitReader(f, size)), nil
	}

	return nil, errors.New("blob storage does not support range reads")
}

func (b *blobStore) Has(hash []byte) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var exists int
	err := b.db.QueryRow("SELECT 1 FROM blobs WHERE hash = ?", hash).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (b *blobStore) Size(hash []byte) (int64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var size int64
	err := b.db.QueryRow("SELECT size FROM blobs WHERE hash = ?", hash).Scan(&size)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return size, nil
}

func (b *blobStore) Close() error {
	return nil
}

func (b *blobStore) Delete(hash []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	_, err := b.db.Exec("DELETE FROM blobs WHERE hash = ?", hash)
	return err
}

func now() int64 {
	return time.Now().UnixMilli()
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

type MessageVerifier func(msg *legacy.SignedMessage, author *refs.FeedRef) error

func DefaultVerify(msg *legacy.SignedMessage, author *refs.FeedRef) error {
	return msg.Verify()
}

type loggedMessage struct {
	Type string `json:"type"`
}

func parseMessageType(content []byte) (loggedMessage, bool) {
	var msg loggedMessage
	if err := json.Unmarshal(content, &msg); err != nil {
		return msg, false
	}
	return msg, msg.Type != ""
}
