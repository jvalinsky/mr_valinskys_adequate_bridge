package feedlog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

var (
	ErrInvalidMessage = errors.New("feedlog: invalid message")
	ErrSeqNotFound    = errors.New("feedlog: sequence not found")
	ErrFeedNotFound   = errors.New("feedlog: feed not found")
	ErrInvalidContent = errors.New("feedlog: invalid content")
	ErrNotFound       = errors.New("sqlite: not found")
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
}

type Metadata struct {
	Author    string
	Sequence  int64
	Previous  string
	Timestamp int64
	Sig       []byte
	Hash      string
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
	Close() error
}

type BlobStore interface {
	Put(r io.Reader) ([]byte, error)
	Get(hash []byte) (io.ReadCloser, error)
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
	return &multiLog{db: s.db}
}

func (s *StoreImpl) ReceiveLog() (Log, error) {
	return &receiveLog{db: s.db}, nil
}

func (s *StoreImpl) Blobs() BlobStore {
	return &blobStore{db: s.db, blobPath: s.blobPath}
}

func (s *StoreImpl) Close() error {
	return s.db.Close()
}

type multiLog struct {
	db *sql.DB
	mu sync.RWMutex
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
	err := m.db.QueryRow("SELECT id FROM feeds WHERE addr = ?", []byte(addr)).Scan(&feedID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return &logAdapter{db: m.db, feedID: feedID}, nil
}

func (m *multiLog) Create(addr string) (Log, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.db.Exec("INSERT INTO feeds (addr, created_at) VALUES (?, ?)", []byte(addr), now())
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &logAdapter{db: m.db, feedID: id}, nil
}

func (m *multiLog) Has(addr string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var exists int
	err := m.db.QueryRow("SELECT 1 FROM feeds WHERE addr = ?", []byte(addr)).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (m *multiLog) Close() error {
	return nil
}

type logAdapter struct {
	db     *sql.DB
	feedID int64
	mu     sync.RWMutex
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

	var currentSeq sql.NullInt64
	if err := l.db.QueryRow("SELECT MAX(seq) FROM messages WHERE feed_id = ?", l.feedID).Scan(&currentSeq); err != nil {
		return 0, err
	}

	nextSeq := int64(1)
	if currentSeq.Valid {
		nextSeq = currentSeq.Int64 + 1
	}

	key := fmt.Sprintf("%x", data)[:32]

	_, err = l.db.Exec(
		"INSERT INTO messages (feed_id, seq, key, value_json, created_at) VALUES (?, ?, ?, ?, ?)",
		l.feedID, nextSeq, key, data, now(),
	)
	if err != nil {
		return 0, err
	}

	_, err = l.db.Exec(
		"INSERT OR REPLACE INTO messages_key_idx (key, feed_id, seq) VALUES (?, ?, ?)",
		key, l.feedID, nextSeq,
	)
	if err != nil {
		return 0, err
	}

	return nextSeq, nil
}

func (l *logAdapter) Get(seq int64) (*StoredMessage, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var data []byte
	err := l.db.QueryRow("SELECT value_json FROM messages WHERE feed_id = ? AND seq = ?", l.feedID, seq).Scan(&data)
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
	}, nil
}

func (l *logAdapter) Query(specs ...QuerySpec) (Source, error) {
	return nil, errors.New("not implemented")
}

func (l *logAdapter) Close() error {
	return nil
}

type receiveLog struct {
	db *sql.DB
	mu sync.RWMutex
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

	wrapper := &storedMessageWrapper{
		Content:  content,
		Metadata: metadata,
	}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return 0, err
	}

	var currentSeq sql.NullInt64
	if err := l.db.QueryRow("SELECT MAX(seq) FROM receive_log").Scan(&currentSeq); err != nil {
		return 0, err
	}

	nextSeq := int64(1)
	if currentSeq.Valid {
		nextSeq = currentSeq.Int64 + 1
	}

	key := fmt.Sprintf("%x", data)[:32]

	_, err = l.db.Exec(
		"INSERT INTO receive_log (seq, key, value_json, created_at) VALUES (?, ?, ?, ?)",
		nextSeq, key, data, now(),
	)
	if err != nil {
		return 0, err
	}

	return nextSeq, nil
}

func (l *receiveLog) Get(seq int64) (*StoredMessage, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var data []byte
	err := l.db.QueryRow("SELECT value_json FROM receive_log WHERE seq = ?", seq).Scan(&data)
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
	}, nil
}

func (l *receiveLog) Query(specs ...QuerySpec) (Source, error) {
	return nil, errors.New("not implemented")
}

func (l *receiveLog) Close() error {
	return nil
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
