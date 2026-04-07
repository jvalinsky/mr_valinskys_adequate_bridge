package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/storage"
)

var (
	ErrNotFound    = errors.New("sqlite: not found")
	ErrExists      = errors.New("sqlite: already exists")
	ErrInvalidFeed = errors.New("sqlite: invalid feed")
)

type Store struct {
	db       *sql.DB
	blobPath string
	mu       sync.RWMutex
}

type Config struct {
	DBPath     string
	RepoPath   string
	BlobSubdir string
}

func New(cfg Config) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.DBPath+"?_journal_mode=WAL&_busy_timeout=5000")
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

	s := &Store{
		db:       db,
		blobPath: blobPath,
	}

	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
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
		`CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY
		)`,
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

func (s *Store) Logs() storage.MultiLog {
	return &MultiLog{db: s.db}
}

func (s *Store) ReceiveLog() (storage.Log, error) {
	return &ReceiveLog{db: s.db}, nil
}

func (s *Store) BlobStore() storage.BlobStore {
	return &BlobStoreImpl{db: s.db, blobPath: s.blobPath}
}

func (s *Store) Close() error {
	return s.db.Close()
}

type MultiLog struct {
	db *sql.DB
	mu sync.RWMutex
}

func (m *MultiLog) List() ([]storage.FeedAddr, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.Query("SELECT addr FROM feeds ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addrs []storage.FeedAddr
	for rows.Next() {
		var addr []byte
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		addrs = append(addrs, storage.FeedAddr(addr))
	}
	return addrs, rows.Err()
}

func (m *MultiLog) Get(addr storage.FeedAddr) (storage.Log, error) {
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

	return &Log{db: m.db, feedID: feedID}, nil
}

func (m *MultiLog) Create(feed []byte) (storage.Log, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.db.Exec("INSERT INTO feeds (addr, created_at) VALUES (?, ?)", feed, now())
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Log{db: m.db, feedID: id}, nil
}

func (m *MultiLog) Has(addr storage.FeedAddr) (bool, error) {
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

func (m *MultiLog) Close() error {
	return nil
}

type Log struct {
	db     *sql.DB
	feedID int64
	mu     sync.RWMutex
}

func (l *Log) Seq() (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var seq sql.NullInt64
	err := l.db.QueryRow("SELECT MAX(seq) FROM messages WHERE feed_id = ?", l.feedID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return -1, storage.SeqEmpty
	}
	return seq.Int64, nil
}

func (l *Log) Append(msg interface{}) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(msg)
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

	result, err := l.db.Exec(
		"INSERT INTO messages (feed_id, seq, key, value_json, created_at) VALUES (?, ?, ?, ?, ?)",
		l.feedID, nextSeq, key, data, now(),
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
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

	_ = id
	return nextSeq, nil
}

func (l *Log) Get(seq int64) (interface{}, error) {
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

	var msg interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (l *Log) Query(specs ...storage.QuerySpec) (storage.Source, error) {
	return nil, errors.New("not implemented")
}

func (l *Log) Close() error {
	return nil
}

type ReceiveLog struct {
	db *sql.DB
	mu sync.RWMutex
}

func (l *ReceiveLog) Seq() (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var seq sql.NullInt64
	err := l.db.QueryRow("SELECT MAX(seq) FROM receive_log").Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return -1, storage.SeqEmpty
	}
	return seq.Int64, nil
}

func (l *ReceiveLog) Append(msg interface{}) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(msg)
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

func (l *ReceiveLog) Get(seq int64) (interface{}, error) {
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

	var msg interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (l *ReceiveLog) Query(specs ...storage.QuerySpec) (storage.Source, error) {
	return nil, errors.New("not implemented")
}

func (l *ReceiveLog) Close() error {
	return nil
}

type BlobStoreImpl struct {
	db       *sql.DB
	blobPath string
	mu       sync.RWMutex
}

func (b *BlobStoreImpl) Put(r io.Reader) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	hash := sha256Hash(data)

	if b.blobPath != "" {
		blobFile := filepath.Join(b.blobPath, fmt.Sprintf("%x", hash[:1]))
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

func (b *BlobStoreImpl) Get(hash []byte) (io.ReadCloser, error) {
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

func (b *BlobStoreImpl) GetRange(hash []byte, start, size int64) (io.ReadCloser, error) {
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

func (b *BlobStoreImpl) Has(hash []byte) (bool, error) {
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

func (b *BlobStoreImpl) Size(hash []byte) (int64, error) {
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

func (b *BlobStoreImpl) Close() error {
	return nil
}

func now() int64 {
	return 0
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
