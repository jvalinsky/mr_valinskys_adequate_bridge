package bridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
)

type stubReverseWriter struct {
	createCalls []stubReverseCreateCall
	uploadCalls []stubReverseUploadCall
	deleteCalls []string
	createErr   error
	deleteErr   error
	uploadErr   error
	sessionErr  error
	uploadMime  string
	closeUpload bool
}

type stubReverseCreateCall struct {
	cred       reverseResolvedCredential
	collection string
	record     any
}

type stubReverseUploadCall struct {
	cred     reverseResolvedCredential
	payload  []byte
	mimeType string
}

type stubReverseSession struct {
	writer *stubReverseWriter
	cred   reverseResolvedCredential
}

func (w *stubReverseWriter) NewSession(_ context.Context, cred reverseResolvedCredential) (ReverseRecordSession, error) {
	if w.sessionErr != nil {
		return nil, w.sessionErr
	}
	return &stubReverseSession{writer: w, cred: cred}, nil
}

func (s *stubReverseSession) UploadBlob(_ context.Context, input io.Reader, mimeType string) (*lexutil.LexBlob, error) {
	payload, _ := io.ReadAll(input)
	if s.writer.closeUpload {
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	if s.writer.uploadErr != nil {
		return nil, s.writer.uploadErr
	}
	s.writer.uploadCalls = append(s.writer.uploadCalls, stubReverseUploadCall{
		cred:     s.cred,
		payload:  payload,
		mimeType: mimeType,
	})
	uploadedMimeType := s.writer.uploadMime
	if uploadedMimeType == "" {
		uploadedMimeType = "image/png"
	}
	return &lexutil.LexBlob{
		Ref:      mustTestLexLink("bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"),
		MimeType: uploadedMimeType,
		Size:     int64(len(payload)),
	}, nil
}

func (s *stubReverseSession) CreateRecord(_ context.Context, collection string, record any) (*ReverseCreatedRecord, error) {
	return s.writer.createRecord(s.cred, collection, record)
}

func (s *stubReverseSession) DeleteRecord(_ context.Context, atURI string) error {
	return s.writer.deleteRecord(atURI)
}

func (w *stubReverseWriter) createRecord(cred reverseResolvedCredential, collection string, record any) (*ReverseCreatedRecord, error) {
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

func (w *stubReverseWriter) deleteRecord(atURI string) error {
	if w.deleteErr != nil {
		return w.deleteErr
	}
	w.deleteCalls = append(w.deleteCalls, atURI)
	return nil
}

type stubReverseBlobStore struct {
	blobs map[string][]byte
	err   error
}

type stubReverseBlobFetcher struct {
	store    *stubReverseBlobStore
	payloads map[string][]byte
	fetches  []string
	err      error
}

func (s *stubReverseBlobStore) Get(hash []byte) (io.ReadCloser, error) {
	if s.err != nil {
		return nil, s.err
	}
	payload, ok := s.blobs[string(hash)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (s *stubReverseBlobFetcher) EnsureBlob(_ context.Context, _ string, ref *refs.BlobRef) error {
	if s.err != nil {
		return s.err
	}
	if s == nil || s.store == nil || ref == nil {
		return os.ErrNotExist
	}
	s.fetches = append(s.fetches, ref.String())
	payload, ok := s.payloads[ref.String()]
	if !ok {
		return os.ErrNotExist
	}
	if s.store.blobs == nil {
		s.store.blobs = make(map[string][]byte)
	}
	s.store.blobs[string(ref.Hash())] = payload
	return nil
}

func addTestReverseBlob(t *testing.T, store *stubReverseBlobStore, payload []byte) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	ref := refs.MustNewBlobRef(sum[:]).String()
	if store.blobs == nil {
		store.blobs = make(map[string][]byte)
	}
	store.blobs[string(sum[:])] = payload
	return ref
}

func mustTestLexLink(raw string) lexutil.LexLink {
	decoded, err := cid.Decode(raw)
	if err != nil {
		panic(err)
	}
	return lexutil.LexLink(decoded)
}

func newTestReverseProcessor(t *testing.T, database *db.DB, writer *stubReverseWriter, blobStore ...ReverseBlobStore) *ReverseProcessor {
	t.Helper()
	const passwordEnv = "BRIDGE_TEST_REVERSE_PASSWORD"
	if err := os.Setenv(passwordEnv, "secret"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(passwordEnv) })

	var configuredBlobStore ReverseBlobStore
	if len(blobStore) > 0 {
		configuredBlobStore = blobStore[0]
	}

	return NewReverseProcessor(ReverseProcessorConfig{
		DB:        database,
		BlobStore: configuredBlobStore,
		Writer:    writer,
		Logger:    log.New(io.Discard, "", 0),
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

func newTestReverseProcessorWithFetcher(t *testing.T, database *db.DB, writer *stubReverseWriter, blobStore ReverseBlobStore, blobFetcher ReverseBlobFetcher) *ReverseProcessor {
	t.Helper()
	const passwordEnv = "BRIDGE_TEST_REVERSE_PASSWORD"
	if err := os.Setenv(passwordEnv, "secret"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(passwordEnv) })

	return NewReverseProcessor(ReverseProcessorConfig{
		DB:          database,
		BlobStore:   blobStore,
		BlobFetcher: blobFetcher,
		Writer:      writer,
		Logger:      log.New(io.Discard, "", 0),
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

func TestReverseProcessorPublishesPostWithImageEmbedAndFacets(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("png-data"))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 8, MimeType: "image/png"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadMime: "image/png"}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"Hi @bob https://example.com ![Preview](` + blobRef + `)","mentions":[{"link":"@bob.ed25519","name":"@bob"},{"link":"` + blobRef + `","name":"Sunset","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 7, "%post-image.sha256", "@alice.ed25519", int64Ptr(7), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(writer.uploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(writer.uploadCalls))
	}
	if len(writer.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(writer.createCalls))
	}
	post, ok := writer.createCalls[0].record.(*appbsky.FeedPost)
	if !ok {
		t.Fatalf("expected feed post, got %T", writer.createCalls[0].record)
	}
	if post.Text != "Hi @bob https://example.com" {
		t.Fatalf("unexpected shaped text %q", post.Text)
	}
	if post.Embed == nil || post.Embed.EmbedImages == nil || len(post.Embed.EmbedImages.Images) != 1 {
		t.Fatalf("expected one embedded image, got %#v", post.Embed)
	}
	if post.Embed.EmbedImages.Images[0].Alt != "Sunset" {
		t.Fatalf("expected alt text from mention name, got %#v", post.Embed.EmbedImages.Images[0])
	}
	if len(post.Facets) != 2 {
		t.Fatalf("expected mention + link facets, got %#v", post.Facets)
	}
	if post.Facets[0].Features[0].RichtextFacet_Mention == nil || post.Facets[0].Features[0].RichtextFacet_Mention.Did != "did:plc:bob" {
		t.Fatalf("expected mention facet first, got %#v", post.Facets[0])
	}
	if post.Facets[1].Features[0].RichtextFacet_Link == nil || post.Facets[1].Features[0].RichtextFacet_Link.Uri != "https://example.com" {
		t.Fatalf("expected bare URL link facet, got %#v", post.Facets[1])
	}
}

func TestReverseProcessorUsesBlobMetadataMimeFallback(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("jpeg-data"))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 9, MimeType: "image/jpeg"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadMime: "image/jpeg"}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"photo ![Fallback](` + blobRef + `)","mentions":[{"link":"` + blobRef + `","name":"Fallback"}]}`
	if err := proc.processDecodedMessage(ctx, 8, "%post-fallback.sha256", "@alice.ed25519", int64Ptr(8), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(writer.createCalls) != 1 {
		t.Fatalf("expected published create call, got %d", len(writer.createCalls))
	}
	post := writer.createCalls[0].record.(*appbsky.FeedPost)
	if post.Embed == nil || post.Embed.EmbedImages == nil || len(post.Embed.EmbedImages.Images) != 1 {
		t.Fatalf("expected image embed, got %#v", post.Embed)
	}
	if post.Embed.EmbedImages.Images[0].Image.MimeType != "image/jpeg" {
		t.Fatalf("expected mime fallback to image/jpeg, got %#v", post.Embed.EmbedImages.Images[0].Image)
	}
}

func TestReverseProcessorDropsUnmappedFeedMentionFacet(t *testing.T) {
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
	raw := `{"type":"post","text":"Hello @ghost","mentions":[{"link":"@ghost.ed25519","name":"@ghost"}]}`
	if err := proc.processDecodedMessage(ctx, 9, "%post-ghost.sha256", "@alice.ed25519", int64Ptr(9), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	post := writer.createCalls[0].record.(*appbsky.FeedPost)
	if len(post.Facets) != 0 {
		t.Fatalf("expected unresolved feed mention to drop facet, got %#v", post.Facets)
	}
	if post.Text != "Hello @ghost" {
		t.Fatalf("unexpected text %q", post.Text)
	}
}

func TestReverseProcessorFetchesMissingBlobBeforePublishing(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	payload := []byte("png-data")
	blobRef := addTestReverseBlob(t, blobStore, payload)
	blobStore.blobs = map[string][]byte{}
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: int64(len(payload)), MimeType: "image/png"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	blobFetcher := &stubReverseBlobFetcher{
		store:    blobStore,
		payloads: map[string][]byte{blobRef: payload},
	}
	writer := &stubReverseWriter{uploadMime: "image/png"}
	proc := newTestReverseProcessorWithFetcher(t, database, writer, blobStore, blobFetcher)
	raw := `{"type":"post","text":"Hi @bob ![Preview](` + blobRef + `)","mentions":[{"link":"@bob.ed25519","name":"@bob"},{"link":"` + blobRef + `","name":"Sunset","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 10, "%post-fetchblob.sha256", "@alice.ed25519", int64Ptr(10), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	if len(blobFetcher.fetches) != 1 || blobFetcher.fetches[0] != blobRef {
		t.Fatalf("expected blob fetch for %s, got %#v", blobRef, blobFetcher.fetches)
	}
	if len(writer.uploadCalls) != 1 || len(writer.createCalls) != 1 {
		t.Fatalf("expected upload + create after fetch, got uploads=%d creates=%d", len(writer.uploadCalls), len(writer.createCalls))
	}
	event, err := database.GetReverseEvent(ctx, "%post-fetchblob.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStatePublished {
		t.Fatalf("expected published reverse event, got %#v", event)
	}
}

func TestReverseProcessorIgnoresAlreadyClosedBlobReaderAfterUpload(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("png-data"))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 8, MimeType: "image/png"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadMime: "image/png", closeUpload: true}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"![Preview](` + blobRef + `)","mentions":[{"link":"` + blobRef + `","name":"Preview","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 11, "%post-upload-close.sha256", "@alice.ed25519", int64Ptr(11), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	event, err := database.GetReverseEvent(ctx, "%post-upload-close.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStatePublished {
		t.Fatalf("expected published reverse event, got %#v", event)
	}
}

func TestReverseProcessorDefersPostWhenBlobReadFails(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("png-data"))
	sum := sha256.Sum256([]byte("png-data"))
	delete(blobStore.blobs, string(sum[:]))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 8, MimeType: "image/png"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadMime: "image/png"}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"![Preview](` + blobRef + `)","mentions":[{"link":"` + blobRef + `","name":"Preview","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 10, "%post-readfail.sha256", "@alice.ed25519", int64Ptr(10), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	event, err := database.GetReverseEvent(ctx, "%post-readfail.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStateDeferred || !strings.Contains(event.DeferReason, "blob_read_failed="+blobRef) {
		t.Fatalf("unexpected deferred event: %#v", event)
	}
}

func TestReverseProcessorDefersPostWhenBlobUploadFails(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("png-data"))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 8, MimeType: "image/png"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadErr: errors.New("upload failed")}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"![Preview](` + blobRef + `)","mentions":[{"link":"` + blobRef + `","name":"Preview","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 11, "%post-uploadfail.sha256", "@alice.ed25519", int64Ptr(11), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	event, err := database.GetReverseEvent(ctx, "%post-uploadfail.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStateDeferred || !strings.Contains(event.DeferReason, "blob_upload_failed="+blobRef) {
		t.Fatalf("unexpected deferred event: %#v", event)
	}
}

func TestReverseProcessorDefersPostWhenBlobMimeMismatchesMetadata(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("png-data"))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 8, MimeType: "image/jpeg"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadMime: "image/png"}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"![Preview](` + blobRef + `)","mentions":[{"link":"` + blobRef + `","name":"Preview","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 12, "%post-mimefail.sha256", "@alice.ed25519", int64Ptr(12), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	event, err := database.GetReverseEvent(ctx, "%post-mimefail.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStateDeferred || !strings.Contains(event.DeferReason, "blob_mime_mismatch="+blobRef) {
		t.Fatalf("unexpected deferred event: %#v", event)
	}
}

func TestReverseProcessorDefersPostWhenImageLimitExceeded(t *testing.T) {
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

	blobStore := &stubReverseBlobStore{}
	writer := &stubReverseWriter{uploadMime: "image/png"}
	proc := newTestReverseProcessor(t, database, writer, blobStore)

	mentions := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		payload := []byte{byte('a' + i)}
		blobRef := addTestReverseBlob(t, blobStore, payload)
		if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: int64(len(payload)), MimeType: "image/png"}); err != nil {
			t.Fatalf("add blob metadata: %v", err)
		}
		mentions = append(mentions, `{"link":"`+blobRef+`","name":"img","type":"image/png"}`)
	}

	raw := `{"type":"post","text":"album","mentions":[` + strings.Join(mentions, ",") + `]}`
	if err := proc.processDecodedMessage(ctx, 13, "%post-overflow.sha256", "@alice.ed25519", int64Ptr(13), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	event, err := database.GetReverseEvent(ctx, "%post-overflow.sha256")
	if err != nil {
		t.Fatalf("get reverse event: %v", err)
	}
	if event == nil || event.EventState != db.ReverseEventStateDeferred || event.DeferReason != "image_limit_exceeded=5" {
		t.Fatalf("unexpected deferred event: %#v", event)
	}
}

func TestReverseProcessorPublishesReplyWithImageEmbedAndFacets(t *testing.T) {
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
	for _, msg := range []db.Message{
		{ATURI: "at://did:plc:target/app.bsky.feed.post/root", ATCID: "cid-root", ATDID: "did:plc:target", Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished, SSBMsgRef: "%root2.sha256"},
		{ATURI: "at://did:plc:target/app.bsky.feed.post/parent", ATCID: "cid-parent", ATDID: "did:plc:target", Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished, SSBMsgRef: "%parent2.sha256"},
	} {
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("seed message %s: %v", msg.ATURI, err)
		}
	}

	blobStore := &stubReverseBlobStore{}
	blobRef := addTestReverseBlob(t, blobStore, []byte("png-data"))
	if err := database.AddBlob(ctx, db.Blob{ATCID: "cid-image", SSBBlobRef: blobRef, Size: 8, MimeType: "image/png"}); err != nil {
		t.Fatalf("add blob metadata: %v", err)
	}

	writer := &stubReverseWriter{uploadMime: "image/png"}
	proc := newTestReverseProcessor(t, database, writer, blobStore)
	raw := `{"type":"post","text":"@bob reply https://example.com ![img](` + blobRef + `)","root":"%root2.sha256","branch":"%parent2.sha256","mentions":[{"link":"@bob.ed25519","name":"@bob"},{"link":"` + blobRef + `","name":"reply image","type":"image/png"}]}`
	if err := proc.processDecodedMessage(ctx, 14, "%reply-image.sha256", "@alice.ed25519", int64Ptr(14), []byte(raw), false); err != nil {
		t.Fatalf("process decoded message: %v", err)
	}

	post := writer.createCalls[0].record.(*appbsky.FeedPost)
	if post.Reply == nil || post.Embed == nil || post.Embed.EmbedImages == nil || len(post.Embed.EmbedImages.Images) != 1 {
		t.Fatalf("expected reply with image embed, got %#v", post)
	}
	if len(post.Facets) != 2 {
		t.Fatalf("expected mention + URL facets on reply, got %#v", post.Facets)
	}
	if post.Text != "@bob reply https://example.com" {
		t.Fatalf("unexpected reply text %q", post.Text)
	}
}
