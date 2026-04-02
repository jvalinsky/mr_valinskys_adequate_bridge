package firehose

import (
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
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

// commitWire and repoOpWire decode firehose CBOR frames using cid.Cid
// directly to avoid refmt atlas issues with LexLink alias types.
type commitWire struct {
	Blobs    []cid.Cid     `json:"blobs" refmt:"blobs"`
	Blocks   []byte        `json:"blocks,omitempty" refmt:"blocks,omitempty"`
	Commit   cid.Cid       `json:"commit" refmt:"commit"`
	Ops      []*repoOpWire `json:"ops" refmt:"ops"`
	PrevData *cid.Cid      `json:"prevData,omitempty" refmt:"prevData,omitempty"`
	Rebase   bool          `json:"rebase" refmt:"rebase"`
	Repo     string        `json:"repo" refmt:"repo"`
	Rev      string        `json:"rev" refmt:"rev"`
	Seq      int64         `json:"seq" refmt:"seq"`
	Since    *string       `json:"since,omitempty" refmt:"since,omitempty"`
	Time     string        `json:"time" refmt:"time"`
	TooBig   bool          `json:"tooBig" refmt:"tooBig"`
}

type repoOpWire struct {
	Action string   `json:"action" refmt:"action"`
	Cid    *cid.Cid `json:"cid" refmt:"cid"`
	Path   string   `json:"path" refmt:"path"`
	Prev   *cid.Cid `json:"prev,omitempty" refmt:"prev,omitempty"`
}

func init() {
	cbornode.RegisterCborType(EventHeader{})
	cbornode.RegisterCborType(ErrorFrame{})
	cbornode.RegisterCborType(commitWire{})
	cbornode.RegisterCborType(repoOpWire{})
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
		var wire commitWire
		if err := cbornode.DecodeReader(r, &wire); err != nil {
			return nil, fmt.Errorf("decode commit event: %w", err)
		}
		event.RepoCommit = convertCommitWire(&wire)
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

func convertCommitWire(wire *commitWire) *atproto.SyncSubscribeRepos_Commit {
	if wire == nil {
		return nil
	}

	out := &atproto.SyncSubscribeRepos_Commit{
		Blocks: lexutil.LexBytes(wire.Blocks),
		Commit: lexutil.LexLink(wire.Commit),
		Rebase: wire.Rebase,
		Repo:   wire.Repo,
		Rev:    wire.Rev,
		Seq:    wire.Seq,
		Since:  wire.Since,
		Time:   wire.Time,
		TooBig: wire.TooBig,
	}

	if len(wire.Blobs) > 0 {
		out.Blobs = make([]lexutil.LexLink, 0, len(wire.Blobs))
		for _, blob := range wire.Blobs {
			out.Blobs = append(out.Blobs, lexutil.LexLink(blob))
		}
	}

	if wire.PrevData != nil {
		prev := lexutil.LexLink(*wire.PrevData)
		out.PrevData = &prev
	}

	if len(wire.Ops) > 0 {
		out.Ops = make([]*atproto.SyncSubscribeRepos_RepoOp, 0, len(wire.Ops))
		for _, op := range wire.Ops {
			if op == nil {
				continue
			}
			mapped := &atproto.SyncSubscribeRepos_RepoOp{
				Action: op.Action,
				Path:   op.Path,
			}
			if op.Cid != nil {
				c := lexutil.LexLink(*op.Cid)
				mapped.Cid = &c
			}
			if op.Prev != nil {
				p := lexutil.LexLink(*op.Prev)
				mapped.Prev = &p
			}
			out.Ops = append(out.Ops, mapped)
		}
	}

	return out
}
