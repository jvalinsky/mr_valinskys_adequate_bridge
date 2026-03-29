package adapter

import (
	"io"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type Publisher interface {
	Publish(content []byte) (refs.MessageRef, error)
	FeedRef() refs.FeedRef
	Seq() (int64, error)
}

type LogManager interface {
	Get(author string) (feedlog.Log, error)
	Create(author string) (feedlog.Log, error)
	Has(author string) (bool, error)
	List() ([]string, error)
}

type BlobStore interface {
	Put(r io.Reader) ([]byte, error)
	Get(hash []byte) (io.ReadCloser, error)
	Has(hash []byte) (bool, error)
	Size(hash []byte) (int64, error)
}

type KeyPairAdapter interface {
	Sign(msg []byte) ([]byte, error)
	Public() []byte
}

type FeedRefAdapter interface {
	String() string
	Bytes() []byte
}

func ToFeedRefString(ref refs.FeedRef) string {
	return ref.String()
}

func FromFeedRefString(s string) (*refs.FeedRef, error) {
	return refs.ParseFeedRef(s)
}

func ToMessageRefString(ref refs.MessageRef) string {
	return ref.String()
}
