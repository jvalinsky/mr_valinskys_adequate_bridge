package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func TestParseHistoryStreamArgsRejectsConflictingSequenceAndSeq(t *testing.T) {
	_, err := parseHistoryStreamArgs(json.RawMessage(`[{"id":"@AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=.ed25519","sequence":1,"seq":2}]`))
	if err == nil {
		t.Fatal("expected conflicting sequence and seq to fail")
	}
}

func TestCreateHistoryStreamReturnsKeyWrappedSignedMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newHistoryTestStore(t)
	feed := refs.MustNewFeedRef(bytes.Repeat([]byte{0x11}, 32), refs.RefAlgoFeedSSB1)
	msgRef := refs.MustNewMessageRef(bytes.Repeat([]byte{0x22}, 32), refs.RefAlgoMessageSSB1)

	appendHistoryMessage(t, store, feed.String(), []byte(`{"type":"post","text":"hello"}`), &feedlog.Metadata{
		Author:    feed.String(),
		Sequence:  1,
		Timestamp: 12345,
		Hash:      msgRef.String(),
		Sig:       []byte("signature-1"),
	})

	client := openHistoryTestClient(t, ctx, NewHistoryStreamHandler(store))
	src, err := client.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, map[string]interface{}{
		"id":    feed.String(),
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("open createHistoryStream source: %v", err)
	}

	var payload map[string]interface{}
	readHistoryJSONFrame(t, ctx, src, &payload)

	if got := payload["key"]; got != msgRef.String() {
		t.Fatalf("expected wrapper key %q, got %#v", msgRef.String(), got)
	}
	if payload["timestamp"] == nil {
		t.Fatalf("expected wrapper timestamp, got %+v", payload)
	}

	value, ok := payload["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected wrapped signed value object, got %#v", payload["value"])
	}
	if got := value["author"]; got != feed.String() {
		t.Fatalf("expected author %q, got %#v", feed.String(), got)
	}
	if got := value["sequence"]; got != float64(1) {
		t.Fatalf("expected sequence 1, got %#v", got)
	}
	if got := value["hash"]; got != "sha256" {
		t.Fatalf("expected sha256 hash marker, got %#v", got)
	}
	wantSig := base64.StdEncoding.EncodeToString([]byte("signature-1")) + ".sig.ed25519"
	if got := value["signature"]; got != wantSig {
		t.Fatalf("expected signature %q, got %#v", wantSig, got)
	}
}

func TestCreateHistoryStreamSupportsKeysFalseAndSeqAlias(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newHistoryTestStore(t)
	feed := refs.MustNewFeedRef(bytes.Repeat([]byte{0x33}, 32), refs.RefAlgoFeedSSB1)
	msgRef := refs.MustNewMessageRef(bytes.Repeat([]byte{0x44}, 32), refs.RefAlgoMessageSSB1)

	appendHistoryMessage(t, store, feed.String(), []byte(`{"type":"post","text":"hello again"}`), &feedlog.Metadata{
		Author:    feed.String(),
		Sequence:  1,
		Timestamp: 67890,
		Hash:      msgRef.String(),
		Sig:       []byte("signature-2"),
	})

	client := openHistoryTestClient(t, ctx, NewHistoryStreamHandler(store))
	src, err := client.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, map[string]interface{}{
		"id":    feed.String(),
		"seq":   0,
		"keys":  false,
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("open createHistoryStream source: %v", err)
	}

	var payload map[string]interface{}
	readHistoryJSONFrame(t, ctx, src, &payload)
	if _, ok := payload["key"]; ok {
		t.Fatalf("expected keys=false payload without wrapper key: %+v", payload)
	}
	if got := payload["author"]; got != feed.String() {
		t.Fatalf("expected author %q, got %#v", feed.String(), got)
	}
	if got := payload["sequence"]; got != float64(1) {
		t.Fatalf("expected sequence 1, got %#v", got)
	}
}

func TestCreateHistoryStreamLiveOldFalseSkipsBacklog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := newHistoryTestStore(t)
	feed := refs.MustNewFeedRef(bytes.Repeat([]byte{0x55}, 32), refs.RefAlgoFeedSSB1)
	firstRef := refs.MustNewMessageRef(bytes.Repeat([]byte{0x66}, 32), refs.RefAlgoMessageSSB1)
	secondRef := refs.MustNewMessageRef(bytes.Repeat([]byte{0x77}, 32), refs.RefAlgoMessageSSB1)

	appendHistoryMessage(t, store, feed.String(), []byte(`{"type":"post","text":"backlog"}`), &feedlog.Metadata{
		Author:    feed.String(),
		Sequence:  1,
		Timestamp: 111,
		Hash:      firstRef.String(),
		Sig:       []byte("signature-3"),
	})

	client := openHistoryTestClient(t, ctx, NewHistoryStreamHandler(store))
	src, err := client.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, map[string]interface{}{
		"id":    feed.String(),
		"old":   false,
		"live":  true,
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("open createHistoryStream source: %v", err)
	}

	go func() {
		time.Sleep(250 * time.Millisecond)
		appendHistoryMessage(t, store, feed.String(), []byte(`{"type":"post","text":"live"}`), &feedlog.Metadata{
			Author:    feed.String(),
			Sequence:  2,
			Timestamp: 222,
			Hash:      secondRef.String(),
			Sig:       []byte("signature-4"),
		})
	}()

	var payload map[string]interface{}
	readHistoryJSONFrame(t, ctx, src, &payload)
	if got := payload["key"]; got != secondRef.String() {
		t.Fatalf("expected live message key %q, got %#v", secondRef.String(), got)
	}
	value, ok := payload["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected wrapped live signed message, got %#v", payload["value"])
	}
	if got := value["sequence"]; got != float64(2) {
		t.Fatalf("expected live message sequence 2, got %#v", got)
	}
}

func newHistoryTestStore(t *testing.T) *feedlog.StoreImpl {
	t.Helper()

	store, err := feedlog.NewStore(feedlog.Config{
		DBPath:     filepath.Join(t.TempDir(), "history.sqlite"),
		RepoPath:   t.TempDir(),
		BlobSubdir: "blobs",
	})
	if err != nil {
		t.Fatalf("new history test store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func appendHistoryMessage(t *testing.T, store *feedlog.StoreImpl, author string, content []byte, metadata *feedlog.Metadata) {
	t.Helper()

	log, err := store.Logs().Create(author)
	if err == nil {
		_, err = log.Append(content, metadata)
		if err != nil {
			t.Fatalf("append history message: %v", err)
		}
		return
	}

	log, err = store.Logs().Get(author)
	if err != nil {
		t.Fatalf("get history log: %v", err)
	}
	if _, err := log.Append(content, metadata); err != nil {
		t.Fatalf("append history message: %v", err)
	}
}

func openHistoryTestClient(t *testing.T, ctx context.Context, handler muxrpc.Handler) *muxrpc.Server {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	server := muxrpc.NewServer(ctx, serverConn, handler, nil)
	client := muxrpc.NewServer(ctx, clientConn, nil, nil)
	t.Cleanup(func() {
		_ = client.Terminate()
		_ = server.Terminate()
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	return client
}

func readHistoryJSONFrame(t *testing.T, ctx context.Context, src *muxrpc.ByteSource, dst interface{}) {
	t.Helper()

	if !src.Next(ctx) {
		if err := src.Err(); err != nil {
			t.Fatalf("source next failed: %v", err)
		}
		t.Fatal("source closed before next frame")
	}

	body, err := src.Bytes()
	if err != nil {
		t.Fatalf("read source bytes: %v", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decode source frame %q: %v", string(body), err)
	}
}
