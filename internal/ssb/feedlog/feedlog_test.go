package feedlog

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
)

func TestStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "feedlog-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.sqlite")
	store, err := NewStore(Config{
		DBPath:     dbPath,
		RepoPath:   tempDir,
		BlobSubdir: "blobs",
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Test MultiLog
	logs := store.Logs()
	author := "@alice.ed25519"

	has, err := logs.Has(author)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected author not to exist yet")
	}

	l, err := logs.Create(author)
	if err != nil {
		t.Fatalf("failed to create log: %v", err)
	}

	has, err = logs.Has(author)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected author to exist")
	}

	list, err := logs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != author {
		t.Errorf("unexpected list: %v", list)
	}

	// Test Log operations
	seq, err := l.Seq()
	if err != nil {
		t.Fatal(err)
	}
	if seq != -1 {
		t.Errorf("expected -1 for empty log, got %d", seq)
	}

	content := []byte(`{"type":"post","text":"hello"}`)
	metadata := &Metadata{
		Author:    author,
		Sequence:  1,
		Timestamp: 123456789,
		Hash:      "%hash1",
		Sig:       []byte("signature"),
	}

	newSeq, err := l.Append(content, metadata)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if newSeq != 1 {
		t.Errorf("expected seq 1, got %d", newSeq)
	}

	seq, err = l.Seq()
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}

	msg, err := l.Get(1)
	if err != nil {
		t.Fatalf("failed to get msg: %v", err)
	}
	if !bytes.Equal(msg.Value, content) {
		t.Errorf("unexpected content: %s", msg.Value)
	}
	if msg.Metadata.Author != author {
		t.Errorf("unexpected author: %s", msg.Metadata.Author)
	}

	// Test ReceiveLog
	rl, err := store.ReceiveLog()
	if err != nil {
		t.Fatal(err)
	}

	rlSeq, err := rl.Seq()
	if err != nil {
		t.Fatal(err)
	}
	if rlSeq != -1 {
		t.Errorf("expected -1 for empty receive log, got %d", rlSeq)
	}

	rlNewSeq, err := rl.Append(content, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if rlNewSeq != 1 {
		t.Errorf("expected seq 1, got %d", rlNewSeq)
	}

	rlMsg, err := rl.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rlMsg.Value, content) {
		t.Errorf("unexpected receive log content: %s", rlMsg.Value)
	}

	// Test BlobStore
	bs := store.Blobs()
	blobContent := []byte("fake blob data")
	hash, err := bs.Put(bytes.NewReader(blobContent))
	if err != nil {
		t.Fatalf("failed to put blob: %v", err)
	}

	hasBlob, err := bs.Has(hash)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlob {
		t.Fatal("expected blob to exist")
	}

	size, err := bs.Size(hash)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(blobContent)) {
		t.Errorf("expected size %d, got %d", len(blobContent), size)
	}

	rc, err := bs.Get(hash)
	if err != nil {
		t.Fatal(err)
	}
	gotBlob, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBlob, blobContent) {
		t.Errorf("unexpected blob content: %s", gotBlob)
	}

	err = bs.Delete(hash)
	if err != nil {
		t.Fatal(err)
	}
	hasBlob, _ = bs.Has(hash)
	if hasBlob {
		t.Fatal("expected blob to be deleted")
	}
}

func TestSignatureVerification(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sig-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.sqlite")
	store, err := NewStore(Config{
		DBPath:   dbPath,
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	authorRef := kp.FeedRef()

	msg := &legacy.Message{
		Author:    authorRef,
		Sequence:  1,
		Timestamp: 1234567890,
		Hash:      legacy.HashAlgorithm,
		Content:   map[string]interface{}{"type": "post", "text": "hello from signature test"},
	}

	msgRef, sig, err := msg.Sign(kp, nil)
	if err != nil {
		t.Fatal(err)
	}

	signed := &legacy.SignedMessage{
		Previous:  msg.Previous,
		Author:    msg.Author,
		Sequence:  msg.Sequence,
		Timestamp: msg.Timestamp,
		Hash:      msg.Hash,
		Content:   msg.Content,
		Signature: sig,
	}

	signedJSON, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}

	metadata := &Metadata{
		Author:    authorRef.String(),
		Sequence:  1,
		Timestamp: 1234567890,
		Hash:      msgRef.String(),
		Sig:       sig,
	}

	store.SetSignatureVerifier(&DefaultSignatureVerifier{})

	var sigLogEntries []struct {
		author string
		valid  bool
	}
	var sigLogMu sync.Mutex
	store.SetSignatureLogger(func(author string, seq int64, key string, valid bool, err error) {
		sigLogMu.Lock()
		defer sigLogMu.Unlock()
		sigLogEntries = append(sigLogEntries, struct {
			author string
			valid  bool
		}{author: author, valid: valid})
	})

	rl, err := store.ReceiveLog()
	if err != nil {
		t.Fatal(err)
	}

	seq, err := rl.Append(signedJSON, metadata)
	if err != nil {
		t.Fatalf("failed to append with valid signature: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}

	sigLogMu.Lock()
	if len(sigLogEntries) != 1 {
		t.Errorf("expected 1 signature log entry, got %d", len(sigLogEntries))
	} else if !sigLogEntries[0].valid {
		t.Error("expected valid signature to be logged as valid")
	}
	sigLogMu.Unlock()
}

func TestNoSignatureVerifier(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nosig-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.sqlite")
	store, err := NewStore(Config{
		DBPath:   dbPath,
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	content := []byte(`{"type":"post","text":"no verifier"}`)
	metadata := &Metadata{
		Author:    "@test.ed25519",
		Sequence:  1,
		Timestamp: 123456789,
		Hash:      "%hash1",
		Sig:       nil,
	}

	rl, err := store.ReceiveLog()
	if err != nil {
		t.Fatal(err)
	}

	seq, err := rl.Append(content, metadata)
	if err != nil {
		t.Fatalf("failed to append without verifier: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}
}

func createTestSignedMessage(t *testing.T, kp *keys.KeyPair, seq int64) ([]byte, *Metadata) {
	t.Helper()

	authorRef := kp.FeedRef()

	msg := &legacy.Message{
		Author:    authorRef,
		Sequence:  seq,
		Timestamp: 1234567890,
		Hash:      legacy.HashAlgorithm,
		Content:   map[string]interface{}{"type": "post", "text": "test message"},
	}

	msgRef, sig, err := msg.Sign(kp, nil)
	if err != nil {
		t.Fatal(err)
	}

	signed := &legacy.SignedMessage{
		Author:    msg.Author,
		Sequence:  msg.Sequence,
		Timestamp: msg.Timestamp,
		Hash:      msg.Hash,
		Content:   msg.Content,
		Signature: sig,
	}

	signedJSON, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}

	metadata := &Metadata{
		Author:    authorRef.String(),
		Sequence:  seq,
		Timestamp: 1234567890,
		Hash:      msgRef.String(),
		Sig:       sig,
	}

	return signedJSON, metadata
}
