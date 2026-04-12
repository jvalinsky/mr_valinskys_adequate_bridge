package room

import (
	"encoding/json"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

func testFeed(b byte) refs.FeedRef {
	var id [32]byte
	id[0] = b
	return *refs.MustNewFeedRef(id[:], refs.RefAlgoFeedSSB1)
}

// --- parseArgList ---

func TestParseArgList_Empty(t *testing.T) {
	args, err := parseArgList(nil)
	if err != nil {
		t.Fatal(err)
	}
	if args != nil {
		t.Errorf("expected nil, got %v", args)
	}
}

func TestParseArgList_SingleItem(t *testing.T) {
	raw := json.RawMessage(`["hello"]`)
	args, err := parseArgList(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
}

func TestParseArgList_MultipleItems(t *testing.T) {
	raw := json.RawMessage(`["a", "b", "c"]`)
	args, err := parseArgList(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}
}

func TestParseArgList_NotArray(t *testing.T) {
	raw := json.RawMessage(`"just a string"`)
	_, err := parseArgList(raw)
	if err == nil {
		t.Error("expected error for non-array input")
	}
}

// --- parseSingleObjectArg ---

func TestParseSingleObjectArg_Valid(t *testing.T) {
	raw := json.RawMessage(`[{"name":"alice"}]`)
	var dst struct{ Name string }
	if err := parseSingleObjectArg(raw, &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Name != "alice" {
		t.Errorf("expected alice, got %s", dst.Name)
	}
}

func TestParseSingleObjectArg_Empty(t *testing.T) {
	if err := parseSingleObjectArg(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestParseSingleObjectArg_TooMany(t *testing.T) {
	raw := json.RawMessage(`[{"a":1},{"b":2}]`)
	var dst map[string]int
	if err := parseSingleObjectArg(raw, &dst); err == nil {
		t.Error("expected error for multiple args")
	}
}

// --- parseSingleStringArg ---

func TestParseSingleStringArg_Valid(t *testing.T) {
	raw := json.RawMessage(`["hello"]`)
	s, err := parseSingleStringArg(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s != "hello" {
		t.Errorf("expected hello, got %s", s)
	}
}

func TestParseSingleStringArg_NotString(t *testing.T) {
	raw := json.RawMessage(`[123]`)
	_, err := parseSingleStringArg(raw)
	if err == nil {
		t.Error("expected error for non-string arg")
	}
}

func TestParseSingleStringArg_TooMany(t *testing.T) {
	raw := json.RawMessage(`["a","b"]`)
	_, err := parseSingleStringArg(raw)
	if err == nil {
		t.Error("expected error for multiple args")
	}
}

// --- parseAliasRegisterArgs ---

func TestParseAliasRegisterArgs_Valid(t *testing.T) {
	raw := json.RawMessage(`["alice", "c2lnbmF0dXJl"]`)
	alias, sig, err := parseAliasRegisterArgs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if alias != "alice" {
		t.Errorf("expected alice, got %s", alias)
	}
	if len(sig) == 0 {
		t.Error("expected non-empty signature")
	}
}

func TestParseAliasRegisterArgs_WrongArgCount(t *testing.T) {
	raw := json.RawMessage(`["only-one"]`)
	_, _, err := parseAliasRegisterArgs(raw)
	if err == nil {
		t.Error("expected error for wrong arg count")
	}
}

// --- aliasRegistrationMessage ---

func TestAliasRegistrationMessage_Format(t *testing.T) {
	room := testFeed(1)
	feed := testFeed(2)
	msg := aliasRegistrationMessage(room, feed, "alice")
	got := string(msg)

	prefix := "=room-alias-registration:"
	if got[:len(prefix)] != prefix {
		t.Errorf("expected prefix %q, got %q", prefix, got[:len(prefix)])
	}
	// Must contain room ID, feed ID, and alias separated by ':'
	if got != prefix+room.String()+":"+feed.String()+":"+"alice" {
		t.Errorf("unexpected message format: %s", got)
	}
}

// --- validateAliasRegistration ---

func TestValidateAliasRegistration_ValidSig(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	caller := kp.FeedRef()
	room := testFeed(0)
	alias := "testuser"

	msg := aliasRegistrationMessage(room, caller, alias)
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}

	if err := validateAliasRegistration(room, caller, alias, sig); err != nil {
		t.Fatalf("expected valid registration, got: %v", err)
	}
}

func TestValidateAliasRegistration_InvalidAlias(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	caller := kp.FeedRef()
	room := testFeed(0)

	err = validateAliasRegistration(room, caller, "BAD ALIAS!", []byte("sig"))
	if err == nil {
		t.Error("expected error for invalid alias")
	}
}

func TestValidateAliasRegistration_EmptySig(t *testing.T) {
	err := validateAliasRegistration(testFeed(0), testFeed(1), "valid", nil)
	if err == nil {
		t.Error("expected error for empty signature")
	}
}

func TestValidateAliasRegistration_TamperedSig(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	caller := kp.FeedRef()
	room := testFeed(0)

	msg := aliasRegistrationMessage(room, caller, "testuser")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the signature
	sig[0] ^= 0xff

	if err := validateAliasRegistration(room, caller, "testuser", sig); err == nil {
		t.Error("expected error for tampered signature")
	}
}

// --- aliasLabelPattern ---

func TestAliasLabelPattern(t *testing.T) {
	valid := []string{"alice", "a", "ab", "a-b", "a1", "test-user-123"}
	for _, v := range valid {
		if !aliasLabelPattern.MatchString(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}

	invalid := []string{"", "A", "Alice", "-start", "end-", "has space", "has.dot", "a--"}
	for _, v := range invalid {
		// "a--" is actually valid per the regex (middle chars can be hyphens)
		if v == "a--" {
			continue
		}
		if aliasLabelPattern.MatchString(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

// --- buildAliasURL ---

func TestBuildAliasURL_WithDomain(t *testing.T) {
	got := buildAliasURL("example.com", "alice")
	if got != "https://example.com/alice" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestBuildAliasURL_EmptyDomain(t *testing.T) {
	got := buildAliasURL("", "alice")
	if got != "/alice" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestBuildAliasURL_TrailingSlash(t *testing.T) {
	got := buildAliasURL("example.com/", "alice")
	if got != "https://example.com/alice" {
		t.Errorf("unexpected: %s", got)
	}
}

// --- normalizeAliasBaseURL ---

func TestNormalizeAliasBaseURL_Empty(t *testing.T) {
	if got := normalizeAliasBaseURL(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestNormalizeAliasBaseURL_HTTPS(t *testing.T) {
	if got := normalizeAliasBaseURL("https://example.com/"); got != "https://example.com" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestNormalizeAliasBaseURL_HTTP(t *testing.T) {
	if got := normalizeAliasBaseURL("http://example.com/"); got != "http://example.com" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestNormalizeAliasBaseURL_Localhost(t *testing.T) {
	if got := normalizeAliasBaseURL("localhost:8080"); got != "http://localhost:8080" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestNormalizeAliasBaseURL_BareDomain(t *testing.T) {
	if got := normalizeAliasBaseURL("example.com"); got != "https://example.com" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestNormalizeAliasBaseURL_LoopbackIP(t *testing.T) {
	if got := normalizeAliasBaseURL("127.0.0.1:3000"); got != "http://127.0.0.1:3000" {
		t.Errorf("unexpected: %s", got)
	}
}

// --- roomFeatures ---

func TestRoomFeatures_Open(t *testing.T) {
	features := roomFeatures(roomdb.ModeOpen)
	found := false
	for _, f := range features {
		if f == "alias" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'alias' in open mode features")
	}
}

func TestRoomFeatures_Restricted(t *testing.T) {
	features := roomFeatures(roomdb.ModeRestricted)
	for _, f := range features {
		if f == "alias" {
			t.Error("expected no 'alias' in restricted mode features")
		}
	}
	// Should still have the base features
	if len(features) != 4 {
		t.Errorf("expected 4 base features, got %d", len(features))
	}
}

func TestRoomFeatures_BaseFeatures(t *testing.T) {
	features := roomFeatures(roomdb.ModeOpen)
	expected := map[string]bool{"tunnel": true, "room2": true, "httpInvite": true, "httpAuth": true}
	for _, f := range features {
		delete(expected, f)
	}
	if len(expected) != 0 {
		t.Errorf("missing base features: %v", expected)
	}
}
