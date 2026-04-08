package refs

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestFeedRefString(t *testing.T) {
	ref := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	s := ref.String()

	if s[0] != SigilFeed {
		t.Errorf("expected feed sigil @, got %c", s[0])
	}

	if len(s) < 9 || s[len(s)-8:] != ".ed25519" {
		t.Errorf("expected suffix .ed25519, got %s", s[len(s)-8:])
	}
}

func TestFeedRefParse(t *testing.T) {
	original := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)

	parsed, err := ParseFeedRef(original.String())
	if err != nil {
		t.Fatalf("failed to parse feed ref: %v", err)
	}

	if !parsed.Equal(*original) {
		t.Errorf("parsed ref doesn't match original")
	}
}

func TestFeedRefParseB64(t *testing.T) {
	ref := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)

	parsed, err := ParseFeedRefB64("YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	if err != nil {
		t.Fatalf("failed to parse feed ref from b64: %v", err)
	}

	if !parsed.Equal(*ref) {
		t.Errorf("parsed ref doesn't match")
	}
}

func TestMessageRefString(t *testing.T) {
	ref := MustNewMessageRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoMessageSSB1)
	s := ref.String()

	if s[0] != SigilMessage {
		t.Errorf("expected message sigil %%, got %c", s[0])
	}

	if len(s) < 7 || s[len(s)-7:] != ".sha256" {
		t.Errorf("expected suffix .sha256, got %s", s[len(s)-7:])
	}
}

func TestMessageRefParse(t *testing.T) {
	original := MustNewMessageRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoMessageSSB1)

	parsed, err := ParseMessageRef(original.String())
	if err != nil {
		t.Fatalf("failed to parse message ref: %v", err)
	}

	if !parsed.Equal(*original) {
		t.Errorf("parsed ref doesn't match original")
	}
}

func TestMessageRefJSON(t *testing.T) {
	original := MustNewMessageRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoMessageSSB1)

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var parsed MessageRef
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}

	if !parsed.Equal(*original) {
		t.Errorf("JSON roundtrip failed")
	}
}

func TestBlobRefString(t *testing.T) {
	ref := MustNewBlobRef([]byte("abcdefghijklmnopqrstuvwxyz123456"))
	s := ref.String()

	if s[0] != SigilBlob {
		t.Errorf("expected blob sigil &, got %c", s[0])
	}

	if len(s) < 7 || s[len(s)-7:] != ".sha256" {
		t.Errorf("expected suffix .sha256, got %s", s[len(s)-7:])
	}
}

func TestBlobRefParse(t *testing.T) {
	original := MustNewBlobRef([]byte("abcdefghijklmnopqrstuvwxyz123456"))

	parsed, err := ParseBlobRef(original.String())
	if err != nil {
		t.Fatalf("failed to parse blob ref: %v", err)
	}

	if !parsed.Equal(*original) {
		t.Errorf("parsed ref doesn't match original")
	}
}

func TestFeedRefEqual(t *testing.T) {
	ref1 := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	ref2 := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	ref3 := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedBendyButt)

	if !ref1.Equal(*ref2) {
		t.Errorf("ref1 should equal ref2")
	}

	if ref1.Equal(*ref3) {
		t.Errorf("ref1 should not equal ref3 (different algo)")
	}
}

func TestInvalidFeedRef(t *testing.T) {
	_, err := ParseFeedRef("not-a-feed-ref")
	if err == nil {
		t.Errorf("expected error parsing invalid feed ref")
	}

	_, err = ParseFeedRef("%abc123=.sha256")
	if err == nil {
		t.Errorf("expected error parsing wrong sigil as feed ref")
	}
}

func TestInvalidMessageRef(t *testing.T) {
	_, err := ParseMessageRef("not-a-message-ref")
	if err == nil {
		t.Errorf("expected error parsing invalid message ref")
	}

	_, err = ParseMessageRef("@abc123=.ed25519")
	if err == nil {
		t.Errorf("expected error parsing wrong sigil as message ref")
	}
}

func TestInvalidBlobRef(t *testing.T) {
	_, err := ParseBlobRef("not-a-blob-ref")
	if err == nil {
		t.Errorf("expected error parsing invalid blob ref")
	}

	_, err = ParseBlobRef("@abc123=.ed25519")
	if err == nil {
		t.Errorf("expected error parsing wrong sigil as blob ref")
	}
}

func TestFeedRefJSON(t *testing.T) {
	original := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var parsed FeedRef
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}

	if !parsed.Equal(*original) {
		t.Errorf("JSON roundtrip failed")
	}
}

func TestSSBURI(t *testing.T) {
	alice := MustNewFeedRef(make([]byte, 32), RefAlgoFeedSSB1)
	furi := &FeedURI{Ref: alice}
	s := furi.String()

	parsed, err := ParseSSBURI(s)
	if err != nil {
		t.Fatalf("failed to parse feed URI: %v", err)
	}

	if parsed.Type() != URITypeFeed {
		t.Errorf("expected feed type, got %v", parsed.Type())
	}

	if !parsed.(*FeedURI).Ref.Equal(*alice) {
		t.Errorf("parsed ref doesn't match")
	}
}

func TestSSBURIUsesCanonicalRawBytes(t *testing.T) {
	feed := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	msg := MustNewMessageRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoMessageSSB1)
	blob := MustNewBlobRef([]byte("abcdefghijklmnopqrstuvwxyz123456"))

	if got, want := (&FeedURI{Ref: feed}).String(), "ssb:feed/classic/"+base64.URLEncoding.EncodeToString(feed.PubKey()); got != want {
		t.Fatalf("feed URI mismatch: got %q want %q", got, want)
	}
	if got, want := (&MessageURI{Ref: msg}).String(), "ssb:message/classic/"+base64.URLEncoding.EncodeToString(msg.Hash()); got != want {
		t.Fatalf("message URI mismatch: got %q want %q", got, want)
	}
	if got, want := (&BlobURI{Ref: blob}).String(), "ssb:blob/classic/"+base64.URLEncoding.EncodeToString(blob.Hash()); got != want {
		t.Fatalf("blob URI mismatch: got %q want %q", got, want)
	}
}

func TestParseSSBURIAcceptsColonSeparatedCanonicalForms(t *testing.T) {
	feed := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	uri := "ssb:feed:classic:" + base64.URLEncoding.EncodeToString(feed.PubKey())

	parsed, err := ParseSSBURI(uri)
	if err != nil {
		t.Fatalf("failed to parse colon-separated feed URI: %v", err)
	}
	if !parsed.(*FeedURI).Ref.Equal(*feed) {
		t.Fatalf("parsed feed ref mismatch")
	}
}

func TestParseSSBURIAcceptsDeprecatedAlgorithmAliases(t *testing.T) {
	feed := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	msg := MustNewMessageRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoMessageSSB1)
	blob := MustNewBlobRef([]byte("abcdefghijklmnopqrstuvwxyz123456"))

	feedURI := "ssb:feed/ed25519/" + base64.URLEncoding.EncodeToString(feed.PubKey())
	parsedFeed, err := ParseSSBURI(feedURI)
	if err != nil {
		t.Fatalf("failed to parse deprecated feed URI: %v", err)
	}
	if !parsedFeed.(*FeedURI).Ref.Equal(*feed) {
		t.Fatalf("parsed deprecated feed ref mismatch")
	}

	msgURI := "ssb:message/sha256/" + base64.URLEncoding.EncodeToString(msg.Hash())
	parsedMsg, err := ParseSSBURI(msgURI)
	if err != nil {
		t.Fatalf("failed to parse deprecated message URI: %v", err)
	}
	if !parsedMsg.(*MessageURI).Ref.Equal(*msg) {
		t.Fatalf("parsed deprecated message ref mismatch")
	}

	blobURI := "ssb:blob/sha256/" + base64.URLEncoding.EncodeToString(blob.Hash())
	parsedBlob, err := ParseSSBURI(blobURI)
	if err != nil {
		t.Fatalf("failed to parse deprecated blob URI: %v", err)
	}
	if !parsedBlob.(*BlobURI).Ref.Equal(*blob) {
		t.Fatalf("parsed deprecated blob ref mismatch")
	}
}

func TestParseSSBURIRejectsLegacyTextPayloads(t *testing.T) {
	feed := MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), RefAlgoFeedSSB1)
	legacyPayload := base64.URLEncoding.EncodeToString([]byte(feed.String()[1:]))
	uri := "ssb:feed/classic/" + legacyPayload

	if _, err := ParseSSBURI(uri); err == nil {
		t.Fatalf("expected legacy text payload to be rejected")
	}
}
