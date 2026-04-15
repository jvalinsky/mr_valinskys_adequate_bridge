package sbot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/bfe"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/bendy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
)

const tildefriendsContactFixture = `{"previous":null,"author":"@6iF2pmL9+jpnM515551HTgVVOGCUZ9qfE8Y3DmdFz7w=.ed25519","sequence":1,"timestamp":1775622534000,"hash":"sha256","content":{"type":"contact","contact":"@HY3zOj73zbLT5wG76eUZXIKTMB4to/voRbYWESXyVtA=.ed25519","following":true},"signature":"IFefnN3fb4bEpWfFtMD2lyn30yQXtmSPVCB0JQQv05WkHVADzz675PiMAf5JLXosTUPfP02IvTeKHdQd1JGPAw==.sig.ed25519"}`
const tildefriendsContactFixtureSeq1 = `{"previous":null,"author":"@XuoXo+OvndX0XptvF6visY2NYzmQP3/o1+vcCs3agnA=.ed25519","sequence":1,"timestamp":1775623664000,"hash":"sha256","content":{"type":"contact","contact":"@Zqr0CSX8DebFQ6esaou2DbTf2gRy66JDfpqiLt6YYSA=.ed25519","following":true},"signature":"bSmTVpr5CBs23EosQO6CDQNSggSvaMB0oK60zQjT2DeVa3UML2wA8/4oo6tj8C3IalaxR5df9jhV4/oWOtS+BQ==.sig.ed25519"}`
const tildefriendsContactFixtureSeq2 = `{"previous":"%ZzaY7xBxIJIubnCl82n4WfM6PE2yI4x0Bz+IaeAl6/A=.sha256","author":"@XuoXo+OvndX0XptvF6visY2NYzmQP3/o1+vcCs3agnA=.ed25519","sequence":2,"timestamp":1775623664000,"hash":"sha256","content":{"type":"contact","contact":"@4+WrdN7ATh8NEswRzK9GF4/Sha5SnMCqV/+eXFFjLb0=.ed25519","following":true},"signature":"rEDIdBFb5D2oy0TnW62gmjRXGx0DeAV+5/77TVrY2553c5awu/JzNvlh9wlfKVxe0wMCR5/g3QOo37Z0aJF4CQ==.sig.ed25519"}`

func TestFeedManagerAdapterAppendSignedMessageStoresReceiveLog(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "feed-manager-adapter-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	store, err := feedlog.NewStore(feedlog.Config{
		DBPath:   filepath.Join(tempDir, "flume.sqlite"),
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	store.SetSignatureVerifier(&feedlog.DefaultSignatureVerifier{})

	adapter := NewFeedManagerAdapter(store)
	fixture := mustPrettyFixture(t, tildefriendsContactFixture)

	author, seq, err := adapter.AppendSignedMessage(fixture)
	if err != nil {
		t.Fatalf("append signed message: %v", err)
	}

	if got, want := author.String(), "@6iF2pmL9+jpnM515551HTgVVOGCUZ9qfE8Y3DmdFz7w=.ed25519"; got != want {
		t.Fatalf("author = %s, want %s", got, want)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}

	wire, err := adapter.GetMessage(author, 1)
	if err != nil {
		t.Fatalf("get classic wire message: %v", err)
	}
	if !bytes.Equal(wire, fixture) {
		t.Fatalf("classic wire bytes mismatch")
	}

	log, err := store.Logs().Get(author.String())
	if err != nil {
		t.Fatalf("get author log: %v", err)
	}
	feedMsg, err := log.Get(1)
	if err != nil {
		t.Fatalf("get feed message: %v", err)
	}
	if got, want := feedMsg.Metadata.Hash, "%+ofkHa7VpmLgrdhkjtY9SFYoOOp+F7KiEHlG9y4s8eo=.sha256"; got != want {
		t.Fatalf("feed metadata hash = %s, want %s", got, want)
	}

	receiveLog, err := store.ReceiveLog()
	if err != nil {
		t.Fatalf("open receive log: %v", err)
	}
	rxSeq, err := receiveLog.Seq()
	if err != nil {
		t.Fatalf("receive log seq: %v", err)
	}
	if rxSeq != 1 {
		t.Fatalf("receive log seq = %d, want 1", rxSeq)
	}
	rxMsg, err := receiveLog.Get(1)
	if err != nil {
		t.Fatalf("get receive log message: %v", err)
	}
	if got, want := rxMsg.Metadata.Hash, "%+ofkHa7VpmLgrdhkjtY9SFYoOOp+F7KiEHlG9y4s8eo=.sha256"; got != want {
		t.Fatalf("receive log metadata hash = %s, want %s", got, want)
	}
}

func TestFeedManagerAdapterAppendSignedMessageAcceptsPreviousRef(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "feed-manager-adapter-seq-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	store, err := feedlog.NewStore(feedlog.Config{
		DBPath:   filepath.Join(tempDir, "flume.sqlite"),
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	store.SetSignatureVerifier(&feedlog.DefaultSignatureVerifier{})

	adapter := NewFeedManagerAdapter(store)
	if _, _, err := adapter.AppendSignedMessage(mustPrettyFixture(t, tildefriendsContactFixtureSeq1)); err != nil {
		t.Fatalf("append seq1: %v", err)
	}
	author, seq, err := adapter.AppendSignedMessage(mustPrettyFixture(t, tildefriendsContactFixtureSeq2))
	if err != nil {
		t.Fatalf("append seq2: %v", err)
	}

	if got, want := author.String(), "@XuoXo+OvndX0XptvF6visY2NYzmQP3/o1+vcCs3agnA=.ed25519"; got != want {
		t.Fatalf("author = %s, want %s", got, want)
	}
	if seq != 2 {
		t.Fatalf("seq = %d, want 2", seq)
	}

	receiveLog, err := store.ReceiveLog()
	if err != nil {
		t.Fatalf("open receive log: %v", err)
	}
	rxSeq, err := receiveLog.Seq()
	if err != nil {
		t.Fatalf("receive log seq: %v", err)
	}
	if rxSeq != 2 {
		t.Fatalf("receive log seq = %d, want 2", rxSeq)
	}
	rxMsg, err := receiveLog.Get(2)
	if err != nil {
		t.Fatalf("get receive log message: %v", err)
	}
	if got, want := rxMsg.Metadata.Previous, "%ZzaY7xBxIJIubnCl82n4WfM6PE2yI4x0Bz+IaeAl6/A=.sha256"; got != want {
		t.Fatalf("receive log previous = %s, want %s", got, want)
	}
	if got, want := rxMsg.Metadata.Hash, "%Qj7wiyE+u7iZaUfMJP01u3Ra10c8SIGdHtKYUqaIg0A=.sha256"; got != want {
		t.Fatalf("receive log metadata hash = %s, want %s", got, want)
	}
}

func TestFeedManagerAdapterAppendReplicatedMessageStoresBendyRawBytes(t *testing.T) {
	tempDir := t.TempDir()
	store, err := feedlog.NewStore(feedlog.Config{
		DBPath:   filepath.Join(tempDir, "flume.sqlite"),
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	pub := kp.Public()
	authorBFE := bfe.EncodeFeed("bendybutt-v1", pub[:])
	msg, err := bendy.CreateMessage(authorBFE, 1, nil, 123, map[string]interface{}{"type": "post", "text": "bendy"}, kp)
	if err != nil {
		t.Fatalf("create bendy message: %v", err)
	}
	raw, err := msg.Encode()
	if err != nil {
		t.Fatalf("encode bendy message: %v", err)
	}
	msgRef, err := msg.ToRefsMessage()
	if err != nil {
		t.Fatalf("message ref: %v", err)
	}

	adapter := NewFeedManagerAdapter(store)
	author, seq, err := adapter.AppendReplicatedMessage(raw)
	if err != nil {
		t.Fatalf("append bendy message: %v", err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}
	if author.Algo() != "bendybutt-v1" {
		t.Fatalf("author algo = %s, want bendybutt-v1", author.Algo())
	}

	log, err := store.Logs().Get(author.String())
	if err != nil {
		t.Fatalf("get bendy log: %v", err)
	}
	stored, err := log.Get(1)
	if err != nil {
		t.Fatalf("get stored bendy message: %v", err)
	}
	if stored.FeedFormat != string(formats.FeedBendyButtV1) || stored.MessageFormat != string(formats.MessageBendyButtV1) {
		t.Fatalf("stored formats = %s/%s", stored.FeedFormat, stored.MessageFormat)
	}
	if stored.Metadata.Hash != msgRef.String() {
		t.Fatalf("stored hash = %s, want %s", stored.Metadata.Hash, msgRef.String())
	}
	if !bytes.Equal(stored.RawValue, raw) {
		t.Fatalf("stored raw bytes mismatch")
	}

	wire, err := adapter.GetMessage(author, 1)
	if err != nil {
		t.Fatalf("get bendy wire message: %v", err)
	}
	if !bytes.Equal(wire, raw) {
		t.Fatalf("GetMessage did not return raw bendy bytes")
	}
}

func mustPrettyFixture(t *testing.T, raw string) []byte {
	t.Helper()
	pretty, err := legacy.PrettyPrint([]byte(raw))
	if err != nil {
		t.Fatalf("pretty print fixture: %v", err)
	}
	return pretty
}
