package firehose

import (
	"bytes"
	"io"
	"testing"

	"github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
)

// encodeFrames encodes header + payload to CBOR and returns a reader
func encodeFrames(t *testing.T, header EventHeader, payload interface{}) io.Reader {
	hBytes, err := cbornode.DumpObject(header)
	if err != nil {
		t.Fatalf("failed to encode header: %v", err)
	}

	var pBytes []byte
	if payload != nil {
		pBytes, err = cbornode.DumpObject(payload)
		if err != nil {
			t.Fatalf("failed to encode payload: %v", err)
		}
	}

	return bytes.NewReader(append(hBytes, pBytes...))
}

// validCID returns a fixed valid CIDv1 for testing
func validCID(t *testing.T) cid.Cid {
	c, err := cid.Decode("bafyreifepiu23okbt5pz7aa5rrql72vgns4ogjzuym7yil7w7edfxz7zcq")
	if err != nil {
		t.Fatalf("failed to create test CID: %v", err)
	}
	return c
}

// TestReadEvent_ErrorFrame tests error frame dispatch
func TestReadEvent_ErrorFrame(t *testing.T) {
	header := EventHeader{Op: EvtKindErrorFrame}
	frame := ErrorFrame{Error: "RateLimitExceeded"}
	r := encodeFrames(t, header, frame)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.Error == nil {
		t.Fatal("error frame is nil")
	}
	if event.Error.Error != "RateLimitExceeded" {
		t.Errorf("wrong error: %s", event.Error.Error)
	}
	if event.Error.Message != nil {
		t.Error("message should be nil")
	}
}

// TestReadEvent_ErrorFrameWithMessage tests error frame with optional message
func TestReadEvent_ErrorFrameWithMessage(t *testing.T) {
	header := EventHeader{Op: EvtKindErrorFrame}
	msg := "Rate limit exceeded, try again later"
	frame := ErrorFrame{Error: "RateLimitExceeded", Message: &msg}
	r := encodeFrames(t, header, frame)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.Error == nil {
		t.Fatal("error frame is nil")
	}
	if event.Error.Message == nil {
		t.Error("message should not be nil")
	}
	if *event.Error.Message != msg {
		t.Errorf("wrong message: %s", *event.Error.Message)
	}
}

// TestReadEvent_CommitBasic tests basic commit frame
func TestReadEvent_CommitBasic(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	wire := commitWire{
		Repo:   "did:plc:abc123",
		Rev:    "rev1",
		Seq:    42,
		Time:   "2024-01-01T00:00:00Z",
		Commit: cidv,
		Blocks: []byte("data"),
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoCommit == nil {
		t.Fatal("RepoCommit is nil")
	}
	if event.RepoCommit.Repo != "did:plc:abc123" {
		t.Errorf("wrong repo: %s", event.RepoCommit.Repo)
	}
	if event.RepoCommit.Seq != 42 {
		t.Errorf("wrong seq: %d", event.RepoCommit.Seq)
	}
	if event.RepoCommit.Time != "2024-01-01T00:00:00Z" {
		t.Errorf("wrong time: %s", event.RepoCommit.Time)
	}
}

// TestReadEvent_CommitWithOps tests commit with operations
func TestReadEvent_CommitWithOps(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	op1 := repoOpWire{Action: "create", Path: "app.bsky.feed.post/123", Cid: &cidv}
	op2 := repoOpWire{Action: "delete", Path: "app.bsky.feed.post/124"}
	wire := commitWire{
		Repo:   "did:plc:test",
		Seq:    1,
		Time:   "2024-01-01T00:00:00Z",
		Commit: cidv,
		Ops:    []*repoOpWire{&op1, &op2},
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if len(event.RepoCommit.Ops) != 2 {
		t.Errorf("expected 2 ops, got %d", len(event.RepoCommit.Ops))
	}
	if event.RepoCommit.Ops[0].Action != "create" {
		t.Errorf("wrong action: %s", event.RepoCommit.Ops[0].Action)
	}
	if event.RepoCommit.Ops[0].Cid == nil {
		t.Error("first op cid should not be nil")
	}
	if event.RepoCommit.Ops[1].Cid != nil {
		t.Error("second op cid should be nil")
	}
}

// TestReadEvent_CommitNilOpSkipped tests that nil ops in slice are skipped
func TestReadEvent_CommitNilOpSkipped(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	op1 := repoOpWire{Action: "create", Path: "path1", Cid: &cidv}
	wire := commitWire{
		Repo:   "did:plc:test",
		Seq:    1,
		Time:   "2024-01-01T00:00:00Z",
		Commit: cidv,
		Ops:    []*repoOpWire{&op1, nil},
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if len(event.RepoCommit.Ops) != 1 {
		t.Errorf("expected 1 op (nil skipped), got %d", len(event.RepoCommit.Ops))
	}
}

// TestReadEvent_CommitPrevData tests pointer preservation for PrevData
func TestReadEvent_CommitPrevData(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	prevCid := validCID(t)
	wire := commitWire{
		Repo:     "did:plc:test",
		Seq:      1,
		Time:     "2024-01-01T00:00:00Z",
		Commit:   cidv,
		PrevData: &prevCid,
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoCommit.PrevData == nil {
		t.Error("PrevData should not be nil")
	}
}

// TestReadEvent_CommitNoPrevData tests nil PrevData
func TestReadEvent_CommitNoPrevData(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	wire := commitWire{
		Repo:   "did:plc:test",
		Seq:    1,
		Time:   "2024-01-01T00:00:00Z",
		Commit: cidv,
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoCommit.PrevData != nil {
		t.Error("PrevData should be nil")
	}
}

// TestReadEvent_CommitBlobsCopied tests blobs are copied
func TestReadEvent_CommitBlobsCopied(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	blob1 := validCID(t)
	blob2 := validCID(t)
	wire := commitWire{
		Repo:   "did:plc:test",
		Seq:    1,
		Time:   "2024-01-01T00:00:00Z",
		Commit: cidv,
		Blobs:  []cid.Cid{blob1, blob2},
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if len(event.RepoCommit.Blobs) != 2 {
		t.Errorf("expected 2 blobs, got %d", len(event.RepoCommit.Blobs))
	}
}

// TestReadEvent_CommitNoBlobs tests empty blobs becomes nil
func TestReadEvent_CommitNoBlobs(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	cidv := validCID(t)
	wire := commitWire{
		Repo:   "did:plc:test",
		Seq:    1,
		Time:   "2024-01-01T00:00:00Z",
		Commit: cidv,
		Blobs:  []cid.Cid{},
	}
	r := encodeFrames(t, header, wire)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoCommit.Blobs != nil {
		t.Error("empty Blobs should be nil, not empty slice")
	}
}

// TestReadEvent_Identity tests identity frame dispatch
func TestReadEvent_Identity(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#identity"}
	identity := atproto.SyncSubscribeRepos_Identity{
		Did: "did:plc:xyz",
		Seq: 10,
	}
	r := encodeFrames(t, header, identity)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoIdentity == nil {
		t.Fatal("RepoIdentity is nil")
	}
	if event.RepoIdentity.Did != "did:plc:xyz" {
		t.Errorf("wrong did: %s", event.RepoIdentity.Did)
	}
}

// TestReadEvent_Account tests account frame dispatch
func TestReadEvent_Account(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#account"}
	account := atproto.SyncSubscribeRepos_Account{
		Did:    "did:plc:xyz",
		Active: true,
		Seq:    5,
	}
	r := encodeFrames(t, header, account)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoAccount == nil {
		t.Fatal("RepoAccount is nil")
	}
	if event.RepoAccount.Did != "did:plc:xyz" {
		t.Errorf("wrong did: %s", event.RepoAccount.Did)
	}
	if !event.RepoAccount.Active {
		t.Error("active should be true")
	}
}

// TestReadEvent_Info tests info frame dispatch
func TestReadEvent_Info(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#info"}
	info := atproto.SyncSubscribeRepos_Info{
		Name: "OutdatedCursor",
	}
	r := encodeFrames(t, header, info)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoInfo == nil {
		t.Fatal("RepoInfo is nil")
	}
	if event.RepoInfo.Name != "OutdatedCursor" {
		t.Errorf("wrong name: %s", event.RepoInfo.Name)
	}
}

// TestReadEvent_Sync tests sync frame dispatch
func TestReadEvent_Sync(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#sync"}
	sync := atproto.SyncSubscribeRepos_Sync{
		Did: "did:plc:xyz",
		Seq: 1,
		Rev: "r1",
	}
	r := encodeFrames(t, header, sync)

	event, err := ReadEvent(r)
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}

	if event.RepoSync == nil {
		t.Fatal("RepoSync is nil")
	}
	if event.RepoSync.Did != "did:plc:xyz" {
		t.Errorf("wrong did: %s", event.RepoSync.Did)
	}
}

// TestReadEvent_UnknownOp tests unknown operation code
func TestReadEvent_UnknownOp(t *testing.T) {
	header := EventHeader{Op: 99}
	r := encodeFrames(t, header, nil)

	_, err := ReadEvent(r)
	if err == nil {
		t.Fatal("expected error for unknown op")
	}
	if err.Error() != "unsupported firehose event op 99" {
		t.Errorf("wrong error message: %v", err)
	}
}

// TestReadEvent_UnknownMsgType tests unknown message type
func TestReadEvent_UnknownMsgType(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#bogus"}
	r := encodeFrames(t, header, nil)

	_, err := ReadEvent(r)
	if err == nil {
		t.Fatal("expected error for unknown message type")
	}
	if err.Error() != `unsupported firehose message type "#bogus"` {
		t.Errorf("wrong error message: %v", err)
	}
}

// TestReadEvent_TruncatedHeader tests malformed header
func TestReadEvent_TruncatedHeader(t *testing.T) {
	r := bytes.NewReader([]byte{0x01})

	_, err := ReadEvent(r)
	if err == nil {
		t.Fatal("expected error for truncated header")
	}
	// Error should wrap "decode firehose header"
	if err.Error() == "" {
		t.Error("error message is empty")
	}
}

// TestReadEvent_TruncatedPayload tests missing payload
func TestReadEvent_TruncatedPayload(t *testing.T) {
	header := EventHeader{Op: EvtKindMessage, MsgType: "#commit"}
	hBytes, _ := cbornode.DumpObject(header)
	r := bytes.NewReader(hBytes) // only header, no payload

	_, err := ReadEvent(r)
	if err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

// TestConvertCommitWire_Nil tests nil input
func TestConvertCommitWire_Nil(t *testing.T) {
	result := convertCommitWire(nil)
	if result != nil {
		t.Error("convertCommitWire(nil) should return nil")
	}
}
