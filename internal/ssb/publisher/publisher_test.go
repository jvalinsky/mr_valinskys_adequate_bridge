package publisher

import (
	"crypto/ed25519"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type mockLog struct {
	seq      int64
	messages map[int64]*feedlog.StoredMessage
}

func (m *mockLog) Seq() (int64, error) { return m.seq, nil }
func (m *mockLog) Append(content []byte, metadata *feedlog.Metadata) (int64, error) {
	if m.seq < 0 {
		m.seq = 1
	} else {
		m.seq++
	}
	m.messages[m.seq] = &feedlog.StoredMessage{
		Value:    content,
		Metadata: metadata,
	}
	return m.seq, nil
}
func (m *mockLog) Get(seq int64) (*feedlog.StoredMessage, error) {
	if msg, ok := m.messages[seq]; ok {
		return msg, nil
	}
	return nil, feedlog.ErrNotFound
}
func (m *mockLog) Query(specs ...feedlog.QuerySpec) (feedlog.Source, error) { return nil, nil }
func (m *mockLog) Close() error                                             { return nil }

type mockMultiLog struct {
	logs map[string]*mockLog
}

func (m *mockMultiLog) List() ([]string, error) { return nil, nil }
func (m *mockMultiLog) Get(author string) (feedlog.Log, error) {
	if l, ok := m.logs[author]; ok {
		return l, nil
	}
	return nil, feedlog.ErrNotFound
}
func (m *mockMultiLog) Create(author string) (feedlog.Log, error) {
	l := &mockLog{seq: -1, messages: make(map[int64]*feedlog.StoredMessage)}
	m.logs[author] = l
	return l, nil
}
func (m *mockMultiLog) Has(author string) (bool, error) {
	_, ok := m.logs[author]
	return ok, nil
}
func (m *mockMultiLog) Close() error { return nil }

func TestPublisher(t *testing.T) {
	aliceKeys, _ := keys.Generate()
	users := &mockMultiLog{logs: make(map[string]*mockLog)}

	p, err := New(aliceKeys, nil, users)
	if err != nil {
		t.Fatal(err)
	}

	content := map[string]interface{}{"type": "post", "text": "hello"}
	ref, err := p.PublishJSON(content)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	if ref.String() == "" {
		t.Error("empty ref")
	}

	seq, _ := p.Seq()
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}

	// Publish second message
	ref2, err := p.PublishJSON(map[string]interface{}{"type": "post", "text": "hello again"})
	if err != nil {
		t.Fatal(err)
	}
	if ref2.String() == ref.String() {
		t.Error("refs should be different")
	}

	seq, _ = p.Seq()
	if seq != 2 {
		t.Errorf("expected seq 2, got %d", seq)
	}
}

func TestPublisherAppendsReceiveLogAndCallsAfterPublish(t *testing.T) {
	aliceKeys, _ := keys.Generate()
	users := &mockMultiLog{logs: make(map[string]*mockLog)}
	receiveLog := &mockLog{seq: -1, messages: make(map[int64]*feedlog.StoredMessage)}

	var callbackFeed refs.FeedRef
	var callbackSeq int64
	p, err := New(
		aliceKeys,
		receiveLog,
		users,
		WithAfterPublish(func(feed refs.FeedRef, seq int64) {
			callbackFeed = feed
			callbackSeq = seq
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := p.PublishJSON(map[string]interface{}{"type": "post", "text": "replicate me"})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	if receiveLog.seq != 1 {
		t.Fatalf("expected receive log seq 1, got %d", receiveLog.seq)
	}
	rxMsg, ok := receiveLog.messages[1]
	if !ok {
		t.Fatal("expected receive log entry at seq 1")
	}
	gotRef, err := legacy.SignedMessageRefFromJSON(rxMsg.Value)
	if err != nil {
		t.Fatalf("derive message ref from receive log: %v", err)
	}
	if gotRef.String() != ref.String() {
		t.Fatalf("receive log ref mismatch: got %q want %q", gotRef.String(), ref.String())
	}
	if callbackSeq != 1 {
		t.Fatalf("after publish seq mismatch: got %d want 1", callbackSeq)
	}
	if !callbackFeed.Equal(aliceKeys.FeedRef()) {
		t.Fatalf("after publish feed mismatch: got %q want %q", callbackFeed.String(), aliceKeys.FeedRef().String())
	}
}

func TestMessageSigning(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	keyPair := keys.FromSeed(*(*[32]byte)(seed[:32]))

	pub := keyPair.Public()

	feedRef, err := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	msg := &legacy.Message{
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test", "text": "hello"},
	}

	msgRef, sig, err := msg.Sign(keyPair, nil)
	if err != nil {
		t.Fatalf("failed to sign message: %v", err)
	}

	if len(sig) != ed25519.SignatureSize {
		t.Errorf("signature length: got %d, want %d", len(sig), ed25519.SignatureSize)
	}

	if msgRef == nil {
		t.Error("message ref is nil")
	}

	contentToSign, err := msg.MarshalForSigning()
	if err != nil {
		t.Fatalf("failed to marshal for signing: %v", err)
	}

	if !ed25519.Verify(pub[:], contentToSign, sig) {
		t.Error("signature verification failed")
	}
}

func TestHMACMessageSigning(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	keyPair := keys.FromSeed(*(*[32]byte)(seed[:32]))
	hmacKey := []byte("test-hmac-key-32-bytes-long!!")

	pub := keyPair.Public()
	feedRef, _ := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)

	msg := &legacy.Message{
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test", "text": "hello with hmac"},
	}

	msgRef, sig, err := msg.Sign(keyPair, hmacKey)
	if err != nil {
		t.Fatalf("failed to sign message with HMAC: %v", err)
	}

	if len(sig) != ed25519.SignatureSize {
		t.Errorf("signature length: got %d, want %d", len(sig), ed25519.SignatureSize)
	}

	if msgRef == nil {
		t.Error("message ref is nil")
	}

	contentToSign, _ := msg.MarshalForSigning()
	h := sha256.New()
	h.Write(hmacKey)
	h.Write(contentToSign)
	hashed := h.Sum(nil)

	if !ed25519.Verify(pub[:], hashed, sig) {
		t.Error("HMAC signature verification failed")
	}
}

func TestKeyDerivation(t *testing.T) {
	masterSeed := make([]byte, 32)
	for i := range masterSeed {
		masterSeed[i] = byte(i + 1)
	}

	did1 := "did:plc:test123"
	did2 := "did:plc:test456"

	kp1, feed1, err := DeriveKeyPair(masterSeed, did1)
	if err != nil {
		t.Fatalf("failed to derive keypair 1: %v", err)
	}

	kp2, feed2, err := DeriveKeyPair(masterSeed, did2)
	if err != nil {
		t.Fatalf("failed to derive keypair 2: %v", err)
	}

	if feed1.Equal(feed2) {
		t.Error("different DIDs should produce different feeds")
	}

	pub1 := kp1.Public()
	pub2 := kp2.Public()

	if string(pub1[:]) == string(pub2[:]) {
		t.Error("different DIDs should produce different keys")
	}

	kp1Again, feed1Again, err := DeriveKeyPair(masterSeed, did1)
	if err != nil {
		t.Fatalf("failed to re-derive keypair: %v", err)
	}

	if !feed1.Equal(feed1Again) {
		t.Error("same DID should produce same feed")
	}

	pub1Again := kp1Again.Public()
	if string(pub1[:]) != string(pub1Again[:]) {
		t.Error("same DID should produce same public key")
	}
}

func TestVerifyMessageCompat(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i * 2)
	}

	keyPair := keys.FromSeed(*(*[32]byte)(seed[:32]))
	pub := keyPair.Public()

	feedRef, _ := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)

	msg := &legacy.Message{
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "about", "name": "Test User"},
	}

	_, sig, err := msg.Sign(keyPair, nil)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	contentToSign, _ := msg.MarshalForSigning()

	if !VerifyMessage(contentToSign, sig, pub[:]) {
		t.Error("message should verify correctly")
	}

	tamperedContent := []byte(`{"author":"` + feedRef.String() + `","sequence":2}`)
	if VerifyMessage(tamperedContent, sig, pub[:]) {
		t.Error("tampered message should not verify")
	}
}
