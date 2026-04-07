package storage

import (
	"context"
	"errors"
	"io"
)

var (
	SeqEmpty = errors.New("log is empty")
)

type QuerySpec interface{}

type Log interface {
	Seq() (int64, error)
	Append(msg interface{}) (int64, error)
	Get(seq int64) (interface{}, error)
	Query(specs ...QuerySpec) (Source, error)
	Close() error
}

type Source interface {
	Next(ctx context.Context) (interface{}, error)
	Close() error
}

type MultiLog interface {
	List() ([]FeedAddr, error)
	Get(addr FeedAddr) (Log, error)
	Create(feed []byte) (Log, error)
	Has(addr FeedAddr) (bool, error)
	Close() error
}

type FeedAddr []byte

type Store interface {
	Logs() MultiLog
	ReceiveLog() (Log, error)
	BlobStore() BlobStore
	Close() error
}

type BlobStore interface {
	Put(r io.Reader) ([]byte, error)
	Get(hash []byte) (io.ReadCloser, error)
	GetRange(hash []byte, start, size int64) (io.ReadCloser, error)
	Has(hash []byte) (bool, error)
	Size(hash []byte) (int64, error)
	Close() error
}

type Config struct {
	DBPath     string
	RepoPath   string
	BlobSubdir string
}
