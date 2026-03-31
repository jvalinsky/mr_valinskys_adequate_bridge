package refs

import (
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
