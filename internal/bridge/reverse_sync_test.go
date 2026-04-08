package bridge

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
)

type stubReverseWriter struct {
	createCalls []stubReverseCreateCall
	deleteCalls []string
	createErr   error
	deleteErr   error
}

type stubReverseCreateCall struct {
	cred       reverseResolvedCredential
	collection string
	record     any
}

func (w *stubReverseWriter) CreateRecord(_ context.Context, cred reverseResolvedCredential, collection string, record any) (*ReverseCreatedRecord, error) {
	if w.createErr != nil {
		return nil, w.createErr
	}
	w.createCalls = append(w.createCalls, stubReverseCreateCall{
		cred:       cred,
		collection: collection,
		record:     record,
	})
	rawRecordJSON, _ := json.Marshal(record)
	return &ReverseCreatedRecord{
		URI:           "at://" + cred.DID + "/" + collection + "/rec" + string(rune('0'+len(w.createCalls))),
		CID:           "cid-" + string(rune('0'+len(w.createCalls))),
		Collection:    collection,
		RawRecordJSON: string(rawRecordJSON),
	}, nil
}

func (w *stubReverseWriter) DeleteRecord(_ context.Context, _ reverseResolvedCredential, atURI string) error {
	if w.deleteErr != nil {
		return w.deleteErr
	}
	w.deleteCalls = append(w.deleteCalls, atURI)
	return nil
}

func newTestReverseProcessor(t *testing.T, database *db.DB, writer *stubReverseWriter) *ReverseProcessor {
	t.Helper()
	const passwordEnv = "BRIDGE_TEST_REVERSE_PASSWORD"
	if err := os.Setenv(passwordEnv, "secret"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(passwordEnv) })

	return NewReverseProcessor(ReverseProcessorConfig{
		DB:     database,
		Writer: writer,
		Logger: log.New(io.Discard, "", 0),
		Credentials: map[string]ReverseCredentialFileEntry{
			"did:plc:alice": {
				Identifier:  "alice.test",
				PDSHost:     "https://pds.example.test",
				PasswordEnv: passwordEnv,
			},
		},
		Enabled: true,
	})
}

func TestReverseProcessorPublishesRootPost(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add mapping: %v", err)
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)
	if err := proc.processDecodedMessage(ctx, 1, "%post.sha256", "@alice.ed25519", int64Ptr(1), []byte(`{"type":"post","text":"hello reverse"}`), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(writer.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(writer.createCalls))
	}
	post, ok := writer.createCalls[0].record.(*appbsky.FeedPost)
	if !ok {
		t.Fatalf("expected feed post, got %T", writer.createCalls[0].record)
	}
	if post.Text != "hello reverse" || post.Reply != nil {
		t.Fatalf("unexpected post payload: %#v", post)
	}

	event, err := database.GetReverseEvent(ctx, "%post.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStatePublished {
		t.Fatalf("expected published reverse event, got %#v", event)
	}

	correlated, err := database.GetMessage(ctx, event.ResultATURI)
	if err != nil {
		t.Fatalf("get correlated message: %v", err)
	}
	if correlated == nil || correlated.SSBMsgRef != "%post.sha256" {
		t.Fatalf("unexpected correlated message: %#v", correlated)
	}
}

func TestReverseProcessorPublishesRootPostFromSignedEnvelope(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add mapping: %v", err)
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)
	rawSigned := []byte(`{"previous":null,"author":"@alice.ed25519","sequence":1,"timestamp":1775624250000,"hash":"sha256","content":{"type":"post","text":"hello signed reverse"},"signature":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==.sig.ed25519"}`)
	if err := proc.processDecodedMessage(ctx, 1, "%post.sha256", "@alice.ed25519", int64Ptr(1), rawSigned, false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(writer.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(writer.createCalls))
	}
	post, ok := writer.createCalls[0].record.(*appbsky.FeedPost)
	if !ok {
		t.Fatalf("expected feed post, got %T", writer.createCalls[0].record)
	}
	if post.Text != "hello signed reverse" {
		t.Fatalf("unexpected post payload: %#v", post)
	}
}

func TestReverseProcessorPublishesReplyWithResolvedTargets(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add author mapping: %v", err)
	}
	for _, msg := range []db.Message{
		{ATURI: "at://did:plc:target/app.bsky.feed.post/root", ATCID: "cid-root", ATDID: "did:plc:target", Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished, SSBMsgRef: "%root.sha256"},
		{ATURI: "at://did:plc:target/app.bsky.feed.post/parent", ATCID: "cid-parent", ATDID: "did:plc:target", Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished, SSBMsgRef: "%parent.sha256"},
	} {
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("seed message %s: %v", msg.ATURI, err)
		}
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)
	if err := proc.processDecodedMessage(ctx, 2, "%reply.sha256", "@alice.ed25519", int64Ptr(2), []byte(`{"type":"post","text":"reply body","root":"%root.sha256","branch":"%parent.sha256"}`), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(writer.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(writer.createCalls))
	}
	post, ok := writer.createCalls[0].record.(*appbsky.FeedPost)
	if !ok {
		t.Fatalf("expected feed post, got %T", writer.createCalls[0].record)
	}
	if post.Reply == nil || post.Reply.Root == nil || post.Reply.Parent == nil {
		t.Fatalf("expected reply refs, got %#v", post)
	}
	if post.Reply.Root.Uri != "at://did:plc:target/app.bsky.feed.post/root" || post.Reply.Parent.Uri != "at://did:plc:target/app.bsky.feed.post/parent" {
		t.Fatalf("unexpected reply refs: %#v", post.Reply)
	}

	event, err := database.GetReverseEvent(ctx, "%reply.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	correlated, err := database.GetMessage(ctx, event.ResultATURI)
	if err != nil {
		t.Fatalf("get correlated message: %v", err)
	}
	if correlated == nil || correlated.RootATURI != "at://did:plc:target/app.bsky.feed.post/root" || correlated.ParentATURI != "at://did:plc:target/app.bsky.feed.post/parent" {
		t.Fatalf("unexpected correlated reply message: %#v", correlated)
	}
}

func TestReverseProcessorDefersReplyWhenTargetsAreUnmapped(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add mapping: %v", err)
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)
	if err := proc.processDecodedMessage(ctx, 3, "%reply-missing.sha256", "@alice.ed25519", int64Ptr(3), []byte(`{"type":"post","text":"reply body","root":"%missing.sha256","branch":"%missing.sha256"}`), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(writer.createCalls) != 0 {
		t.Fatalf("expected no create calls, got %d", len(writer.createCalls))
	}
	event, err := database.GetReverseEvent(ctx, "%reply-missing.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStateDeferred || !strings.Contains(event.DeferReason, "reply_root_unmapped=%missing.sha256") {
		t.Fatalf("unexpected deferred event: %#v", event)
	}
}

func TestReverseProcessorPublishesFollowAndDeletesOnUnfollow(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	for _, mapping := range []db.ReverseIdentityMapping{
		{SSBFeedID: "@alice.ed25519", ATDID: "did:plc:alice", Active: true, AllowPosts: true, AllowReplies: true, AllowFollows: true},
		{SSBFeedID: "@bob.ed25519", ATDID: "did:plc:bob", Active: true, AllowPosts: true, AllowReplies: true, AllowFollows: true},
	} {
		if err := database.AddReverseIdentityMapping(ctx, mapping); err != nil {
			t.Fatalf("add mapping %s: %v", mapping.SSBFeedID, err)
		}
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)
	if err := proc.processDecodedMessage(ctx, 4, "%follow.sha256", "@alice.ed25519", int64Ptr(4), []byte(`{"type":"contact","contact":"@bob.ed25519","following":true,"blocking":false}`), false); err != nil {
		t.Fatalf("process follow: %v", err)
	}
	if len(writer.createCalls) != 1 {
		t.Fatalf("expected follow create call, got %d", len(writer.createCalls))
	}
	follow, ok := writer.createCalls[0].record.(*appbsky.GraphFollow)
	if !ok {
		t.Fatalf("expected graph follow, got %T", writer.createCalls[0].record)
	}
	if follow.Subject != "did:plc:bob" {
		t.Fatalf("unexpected follow subject: %#v", follow)
	}

	followEvent, err := database.GetReverseEvent(ctx, "%follow.sha256")
	if err != nil {
		t.Fatalf("get follow event: %v", err)
	}
	if followEvent == nil || followEvent.EventState != db.ReverseEventStatePublished {
		t.Fatalf("unexpected follow event: %#v", followEvent)
	}

	if err := proc.processDecodedMessage(ctx, 5, "%unfollow.sha256", "@alice.ed25519", int64Ptr(5), []byte(`{"type":"contact","contact":"@bob.ed25519","following":false,"blocking":false}`), false); err != nil {
		t.Fatalf("process unfollow: %v", err)
	}
	if len(writer.deleteCalls) != 1 || writer.deleteCalls[0] != followEvent.ResultATURI {
		t.Fatalf("unexpected delete calls: %#v", writer.deleteCalls)
	}
	unfollowEvent, err := database.GetReverseEvent(ctx, "%unfollow.sha256")
	if err != nil {
		t.Fatalf("get unfollow event: %v", err)
	}
	if unfollowEvent == nil || unfollowEvent.EventState != db.ReverseEventStatePublished {
		t.Fatalf("unexpected unfollow event: %#v", unfollowEvent)
	}
}

func TestReverseProcessorDefersUnfollowWithoutPriorFollowRecord(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	for _, mapping := range []db.ReverseIdentityMapping{
		{SSBFeedID: "@alice.ed25519", ATDID: "did:plc:alice", Active: true, AllowPosts: true, AllowReplies: true, AllowFollows: true},
		{SSBFeedID: "@bob.ed25519", ATDID: "did:plc:bob", Active: true, AllowPosts: true, AllowReplies: true, AllowFollows: true},
	} {
		if err := database.AddReverseIdentityMapping(ctx, mapping); err != nil {
			t.Fatalf("add mapping %s: %v", mapping.SSBFeedID, err)
		}
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)
	if err := proc.processDecodedMessage(ctx, 6, "%unfollow-missing.sha256", "@alice.ed25519", int64Ptr(6), []byte(`{"type":"contact","contact":"@bob.ed25519","following":false,"blocking":false}`), false); err != nil {
		t.Fatalf("process unfollow: %v", err)
	}
	if len(writer.deleteCalls) != 0 {
		t.Fatalf("expected no delete calls, got %#v", writer.deleteCalls)
	}
	event, err := database.GetReverseEvent(ctx, "%unfollow-missing.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStateDeferred || !strings.Contains(event.DeferReason, "follow_record_not_found=did:plc:bob") {
		t.Fatalf("unexpected reverse event: %#v", event)
	}
}
