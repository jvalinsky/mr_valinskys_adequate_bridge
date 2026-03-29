package presentation

import (
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

func TestPrettyJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "valid JSON object",
			raw:  `{"a":1,"b":"hello"}`,
			want: "{\n  \"a\": 1,\n  \"b\": \"hello\"\n}",
		},
		{
			name: "already indented",
			raw:  "{\n  \"x\": true\n}",
			want: "{\n  \"x\": true\n}",
		},
		{
			name: "invalid JSON",
			raw:  `not json at all`,
			want: "not json at all",
		},
		{
			name: "empty string",
			raw:  "",
			want: "(empty)",
		},
		{
			name: "whitespace only",
			raw:  "   \n\t  ",
			want: "(empty)",
		},
		{
			name: "valid JSON array",
			raw:  `[1,2,3]`,
			want: "[\n  1,\n  2,\n  3\n]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrettyJSON(tt.raw)
			if got != tt.want {
				t.Errorf("PrettyJSON(%q) =\n%s\nwant:\n%s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSummarizeATProtoMessage(t *testing.T) {
	tests := []struct {
		name       string
		message    db.Message
		wantLabels []string
	}{
		{
			name: "feed post with text and reply",
			message: db.Message{
				Type:      "app.bsky.feed.post",
				RawATJson: `{"text":"Hello world","createdAt":"2026-03-28T12:00:00Z","reply":{"root":{"uri":"at://did:plc:abc/app.bsky.feed.post/root123"},"parent":{"uri":"at://did:plc:abc/app.bsky.feed.post/parent456"}},"langs":["en","fr"]}`,
			},
			wantLabels: []string{"Collection", "Text", "Created At", "Reply Root", "Reply Parent", "Languages"},
		},
		{
			name: "like record with subject",
			message: db.Message{
				Type:      "app.bsky.feed.like",
				RawATJson: `{"subject":{"uri":"at://did:plc:abc/app.bsky.feed.post/123","cid":"bafyabc"},"createdAt":"2026-03-28T12:00:00Z"}`,
			},
			wantLabels: []string{"Collection", "Created At", "Subject URI"},
		},
		{
			name: "unrecognized fields fall back to payload",
			message: db.Message{
				Type:      "com.example.unknown",
				RawATJson: `{"weird_field": true}`,
			},
			// Only "Collection" from Type; no other recognized keys produce values,
			// but Collection alone is enough to avoid fallback.
			wantLabels: []string{"Collection"},
		},
		{
			name: "invalid JSON falls back",
			message: db.Message{
				Type:      "app.bsky.feed.post",
				RawATJson: `not valid json`,
			},
			wantLabels: []string{"ATProto Payload"},
		},
		{
			name: "empty payload returns nil",
			message: db.Message{
				Type:      "app.bsky.feed.post",
				RawATJson: "",
			},
			wantLabels: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeATProtoMessage(tt.message)
			if tt.wantLabels == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.wantLabels) {
				labels := make([]string, len(got))
				for i, f := range got {
					labels[i] = f.Label
				}
				t.Fatalf("got %d fields %v, want %d labels %v", len(got), labels, len(tt.wantLabels), tt.wantLabels)
			}
			for i, wantLabel := range tt.wantLabels {
				if got[i].Label != wantLabel {
					t.Errorf("field[%d].Label = %q, want %q", i, got[i].Label, wantLabel)
				}
				if got[i].Value == "" {
					t.Errorf("field[%d] (%s) has empty value", i, wantLabel)
				}
			}
		})
	}
}

func TestSummarizeSSBMessage(t *testing.T) {
	tests := []struct {
		name       string
		message    db.Message
		wantLabels []string
	}{
		{
			name: "post type with text and root",
			message: db.Message{
				RawSSBJson: `{"type":"post","text":"Hello SSB","root":"%abc123.sha256","branch":"%def456.sha256"}`,
			},
			wantLabels: []string{"Type", "Text", "Root", "Branch"},
		},
		{
			name: "contact type with following",
			message: db.Message{
				RawSSBJson: `{"type":"contact","_atproto_contact":"did:plc:abc","following":true}`,
			},
			wantLabels: []string{"Type", "Contact DID", "Following"},
		},
		{
			name: "vote type with expression and value",
			message: db.Message{
				RawSSBJson: `{"type":"vote","vote":{"expression":"Like","value":1}}`,
			},
			wantLabels: []string{"Type", "Vote"},
		},
		{
			name: "vote with expression only",
			message: db.Message{
				RawSSBJson: `{"type":"vote","vote":{"expression":"Dig"}}`,
			},
			wantLabels: []string{"Type", "Vote"},
		},
		{
			name: "invalid JSON falls back",
			message: db.Message{
				RawSSBJson: `broken`,
			},
			wantLabels: []string{"SSB Payload"},
		},
		{
			name: "empty payload returns nil",
			message: db.Message{
				RawSSBJson: "",
			},
			wantLabels: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeSSBMessage(tt.message)
			if tt.wantLabels == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.wantLabels) {
				labels := make([]string, len(got))
				for i, f := range got {
					labels[i] = f.Label
				}
				t.Fatalf("got %d fields %v, want %d labels %v", len(got), labels, len(tt.wantLabels), tt.wantLabels)
			}
			for i, wantLabel := range tt.wantLabels {
				if got[i].Label != wantLabel {
					t.Errorf("field[%d].Label = %q, want %q", i, got[i].Label, wantLabel)
				}
				if got[i].Value == "" {
					t.Errorf("field[%d] (%s) has empty value", i, wantLabel)
				}
			}
		})
	}
}

func TestVoteSummary(t *testing.T) {
	tests := []struct {
		name string
		vote interface{}
		want string
	}{
		{
			name: "expression and value",
			vote: map[string]interface{}{"expression": "Like", "value": float64(1)},
			want: "Like (1)",
		},
		{
			name: "expression only",
			vote: map[string]interface{}{"expression": "Dig"},
			want: "Dig",
		},
		{
			name: "value only",
			vote: map[string]interface{}{"value": float64(-1)},
			want: "-1",
		},
		{
			name: "nil input",
			vote: nil,
			want: "",
		},
		{
			name: "wrong type",
			vote: "not a map",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := voteSummary(tt.vote)
			if got != tt.want {
				t.Errorf("voteSummary(%v) = %q, want %q", tt.vote, got, tt.want)
			}
		})
	}
}

func TestDetailCollectorDeduplication(t *testing.T) {
	c := newDetailCollector()
	c.add("Label", "value1")
	c.add("Label", "value1") // duplicate
	c.add("Label", "value2") // same label, different value

	if len(c.fields) != 2 {
		t.Errorf("got %d fields, want 2 (dedup same label+value)", len(c.fields))
	}
}

func TestDetailCollectorSkipsEmpty(t *testing.T) {
	c := newDetailCollector()
	c.add("Label", "")
	c.add("Label", "   ")

	if len(c.fields) != 0 {
		t.Errorf("got %d fields, want 0 (skip empty values)", len(c.fields))
	}
}

func TestJsonScalar(t *testing.T) {
	if s := jsonScalar(nil); s != "" {
		t.Errorf("Expected empty string for nil, got: %s", s)
	}
	if s := jsonScalar(float64(42)); s != "42" {
		t.Errorf("Expected '42' for int, got: %s", s)
	}
	if s := jsonScalar(float64(42.5)); s != "42.5" {
		t.Errorf("Expected '42.5' for float, got: %s", s)
	}
	if s := jsonScalar(true); s != "true" {
		t.Errorf("Expected 'true' for bool, got: %s", s)
	}
}

func TestStringAt(t *testing.T) {
	m := map[string]interface{}{
		"nested": map[string]interface{}{
			"key": "value",
		},
	}
	if s := stringAt(m, "nested.key"); s != "" { // stringAt only works on top level
		t.Errorf("stringAt only works on top level, expected empty, got: %s", s)
	}
	if s := stringAt(m, "notfound"); s != "" {
		t.Errorf("Expected empty string, got: %s", s)
	}
}

func TestPrettyJSONInvalid(t *testing.T) {
	if s := PrettyJSON("invalid json {"); s != "invalid json {" {
		t.Errorf("Expected original string for invalid json, got: %s", s)
	}
}

func TestSummarizeSSBMessageFallback(t *testing.T) {
	msg := db.Message{
		RawSSBJson: `{"unknown": "format"}`,
	}
	fields := SummarizeSSBMessage(msg)
	if len(fields) == 0 {
		t.Errorf("Expected fallback fields for unknown message format")
	}
}
