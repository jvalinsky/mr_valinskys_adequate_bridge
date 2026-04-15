package templates

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderDashboard(t *testing.T) {
	var buf bytes.Buffer
	data := DashboardData{}
	err := RenderDashboard(&buf, data)
	if err != nil {
		t.Fatalf("RenderDashboard failed: %v", err)
	}
}

func TestRenderAccounts(t *testing.T) {
	var buf bytes.Buffer
	data := AccountsData{}
	err := RenderAccounts(&buf, data)
	if err != nil {
		t.Fatalf("RenderAccounts failed: %v", err)
	}
}

func TestRenderMessages(t *testing.T) {
	var buf bytes.Buffer
	data := MessagesData{}
	err := RenderMessages(&buf, data)
	if err != nil {
		t.Fatalf("RenderMessages failed: %v", err)
	}
}

func TestRenderMessageDetail(t *testing.T) {
	var buf bytes.Buffer
	data := MessageDetailData{}
	err := RenderMessageDetail(&buf, data)
	if err != nil {
		t.Fatalf("RenderMessageDetail failed: %v", err)
	}
}

func TestRenderFailures(t *testing.T) {
	var buf bytes.Buffer
	data := FailuresData{}
	err := RenderFailures(&buf, data)
	if err != nil {
		t.Fatalf("RenderFailures failed: %v", err)
	}
}

func TestRenderBlobs(t *testing.T) {
	var buf bytes.Buffer
	data := BlobsData{}
	err := RenderBlobs(&buf, data)
	if err != nil {
		t.Fatalf("RenderBlobs failed: %v", err)
	}
}

func TestRenderState(t *testing.T) {
	var buf bytes.Buffer
	data := StateData{}
	err := RenderState(&buf, data)
	if err != nil {
		t.Fatalf("RenderState failed: %v", err)
	}
}

func TestRenderFeed(t *testing.T) {
	var buf bytes.Buffer
	data := FeedData{
		Chrome: PageChrome{
			ActiveNav: "feed",
			Status:    PageStatus{Visible: false},
		},
		Feed: []FeedRow{
			{
				ATURI:     "at://did:plc/example/app.bsky.feed.post/abc123",
				ATDID:     "did:plc:example",
				Type:      "app.bsky.feed.post",
				CreatedAt: time.Now(),
				Text:      "Hello world",
			},
		},
	}
	err := RenderFeed(&buf, data)
	if err != nil {
		t.Fatalf("RenderFeed failed: %v", err)
	}
}

func TestRenderPost(t *testing.T) {
	var buf bytes.Buffer
	data := PostData{
		Chrome: PageChrome{
			ActiveNav: "post",
			Status:    PageStatus{Visible: false},
		},
		Accounts: []AccountRow{
			{
				ATDID:     "did:plc:example",
				SSBFeedID: "@feed.example.ed25519",
				Active:    true,
			},
		},
	}
	err := RenderPost(&buf, data)
	if err != nil {
		t.Fatalf("RenderPost failed: %v", err)
	}
}

func TestMustPageTemplatePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()
	mustPageTemplate("bad", "{{.BadTemplate") // Missing closing braces
}

func TestMustPageTemplateFuncs(t *testing.T) {
	tmpl := mustPageTemplate("test", `{{define "content"}}{{ fmtTime .Time }} {{ navClass .Active .Tab }} {{ navCurrent .Active .Tab }} {{ statusToneClass .Tone1 }} {{ statusToneClass .Tone2 }} {{ statusToneClass .Tone3 }} {{ statusToneClass .Tone4 }} {{ statusToneClass .Tone5 }} {{ statusToneClass .Tone6 }} {{ statusToneClass .Tone7 }} {{ statusToneClass .Tone8 }} {{ stateClass .State1 }} {{ stateClass .State2 }} {{ stateClass .State3 }} {{ stateClass .State4 }} {{ stateClass .State5 }}{{end}}`)

	var buf bytes.Buffer
	data := struct {
		Chrome PageChrome
		Time   time.Time
		Active string
		Tab    string
		Tone1  string
		Tone2  string
		Tone3  string
		Tone4  string
		Tone5  string
		Tone6  string
		Tone7  string
		Tone8  string
		State1 string
		State2 string
		State3 string
		State4 string
		State5 string
	}{
		Time:   time.Now(),
		Active: "dashboard",
		Tab:    "dashboard",
		Tone1:  "success",
		Tone2:  "warning",
		Tone3:  "danger",
		Tone4:  "other",
		Tone5:  "ingress",
		Tone6:  "egress",
		Tone7:  "bridge",
		Tone8:  "muted",
		State1: "published",
		State2: "failed",
		State3: "deferred",
		State4: "deleted",
		State5: "other",
	}

	err := tmpl.Execute(&buf, data)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	got := buf.String()
	for _, expected := range []string{
		"tone-success",
		"tone-warning",
		"tone-danger",
		"tone-neutral",
		"tone-ingress",
		"tone-egress",
		"tone-bridge",
		"tone-muted",
		"state-published",
		"state-failed",
		"state-deferred",
		"state-deleted",
		"state-pending",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected output to contain %q; got: %q", expected, got)
		}
	}

	// Test zero time and different active tab
	var buf2 bytes.Buffer
	err = tmpl.Execute(&buf2, struct {
		Chrome PageChrome
		Time   time.Time
		Active string
		Tab    string
		Tone1  string
		Tone2  string
		Tone3  string
		Tone4  string
		Tone5  string
		Tone6  string
		Tone7  string
		Tone8  string
		State1 string
		State2 string
		State3 string
		State4 string
		State5 string
	}{
		Time:   time.Time{},
		Active: "dashboard",
		Tab:    "other",
	})
	if err != nil {
		t.Fatalf("Execute zero time failed: %v", err)
	}
}

func TestPageLayoutIncludesProtocolColorTokens(t *testing.T) {
	for _, token := range []string{
		"--proto-at:",
		"--proto-at-deep:",
		"--proto-ssb:",
		"--proto-ssb-deep:",
		"--proto-bridge:",
		"--status-ingress:",
		"--status-egress:",
		"--status-bridge:",
		"--status-success:",
		"--status-failure:",
		".status-strip.tone-ingress",
		".status-strip.tone-egress",
		".status-strip.tone-bridge",
		".pill.state-ingress",
		".pill.state-egress",
		".pill.state-bridge",
	} {
		if !strings.Contains(pageLayout, token) {
			t.Fatalf("pageLayout missing expected token/class %q", token)
		}
	}
}
