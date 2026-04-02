package firehose

import (
	"fmt"
	"io"

	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
)

const (
	EvtKindErrorFrame = -1
	EvtKindMessage    = 1
)

type EventHeader struct {
	Op      int64  `json:"op" refmt:"op"`
	MsgType string `json:"t,omitempty" refmt:"t,omitempty"`
}

type ErrorFrame struct {
	Error   string  `json:"error" refmt:"error"`
	Message *string `json:"message,omitempty" refmt:"message,omitempty"`
}

type Event struct {
	Header       EventHeader
	RepoCommit   *atproto.SyncSubscribeRepos_Commit
	RepoSync     *atproto.SyncSubscribeRepos_Sync
	RepoIdentity *atproto.SyncSubscribeRepos_Identity
	RepoAccount  *atproto.SyncSubscribeRepos_Account
	RepoInfo     *atproto.SyncSubscribeRepos_Info
	Error        *ErrorFrame
}

func init() {
	cbornode.RegisterCborType(EventHeader{})
	cbornode.RegisterCborType(ErrorFrame{})
}

func ReadEvent(r io.Reader) (*Event, error) {
	var header EventHeader
	if err := cbornode.DecodeReader(r, &header); err != nil {
		return nil, fmt.Errorf("decode firehose header: %w", err)
	}

	event := &Event{Header: header}
	switch header.Op {
	case EvtKindErrorFrame:
		var frame ErrorFrame
		if err := cbornode.DecodeReader(r, &frame); err != nil {
			return nil, fmt.Errorf("decode firehose error frame: %w", err)
		}
		event.Error = &frame
		return event, nil
	case EvtKindMessage:
	default:
		return nil, fmt.Errorf("unsupported firehose event op %d", header.Op)
	}

	switch header.MsgType {
	case "#commit":
		var commit atproto.SyncSubscribeRepos_Commit
		if err := cbornode.DecodeReader(r, &commit); err != nil {
			return nil, fmt.Errorf("decode commit event: %w", err)
		}
		event.RepoCommit = &commit
	case "#sync":
		var sync atproto.SyncSubscribeRepos_Sync
		if err := cbornode.DecodeReader(r, &sync); err != nil {
			return nil, fmt.Errorf("decode sync event: %w", err)
		}
		event.RepoSync = &sync
	case "#identity":
		var identity atproto.SyncSubscribeRepos_Identity
		if err := cbornode.DecodeReader(r, &identity); err != nil {
			return nil, fmt.Errorf("decode identity event: %w", err)
		}
		event.RepoIdentity = &identity
	case "#account":
		var account atproto.SyncSubscribeRepos_Account
		if err := cbornode.DecodeReader(r, &account); err != nil {
			return nil, fmt.Errorf("decode account event: %w", err)
		}
		event.RepoAccount = &account
	case "#info":
		var info atproto.SyncSubscribeRepos_Info
		if err := cbornode.DecodeReader(r, &info); err != nil {
			return nil, fmt.Errorf("decode info event: %w", err)
		}
		event.RepoInfo = &info
	default:
		return nil, fmt.Errorf("unsupported firehose message type %q", header.MsgType)
	}

	return event, nil
}
