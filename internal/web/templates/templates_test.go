package templates

import (
	"bytes"
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

func TestMustPageTemplatePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()
	mustPageTemplate("bad", "{{.BadTemplate") // Missing closing braces
}

func TestMustPageTemplateFuncs(t *testing.T) {
	tmpl := mustPageTemplate("test", `{{define "content"}}{{ fmtTime .Time }} {{ navClass .Active .Tab }} {{ navCurrent .Active .Tab }} {{ statusToneClass .Tone1 }} {{ statusToneClass .Tone2 }} {{ statusToneClass .Tone3 }} {{ statusToneClass .Tone4 }} {{ stateClass .State1 }} {{ stateClass .State2 }} {{ stateClass .State3 }} {{ stateClass .State4 }} {{ stateClass .State5 }}{{end}}`)
	
	var buf bytes.Buffer
	data := struct {
		Chrome PageChrome
		Time time.Time
		Active string
		Tab string
		Tone1 string
		Tone2 string
		Tone3 string
		Tone4 string
		State1 string
		State2 string
		State3 string
		State4 string
		State5 string
	}{
		Time: time.Now(),
		Active: "dashboard",
		Tab: "dashboard",
		Tone1: "success",
		Tone2: "warning",
		Tone3: "danger",
		Tone4: "other",
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

	// Test zero time and different active tab
	var buf2 bytes.Buffer
	err = tmpl.Execute(&buf2, struct {
		Chrome PageChrome
		Time time.Time
		Active string
		Tab string
		Tone1 string
		Tone2 string
		Tone3 string
		Tone4 string
		State1 string
		State2 string
		State3 string
		State4 string
		State5 string
	}{
		Time: time.Time{},
		Active: "dashboard",
		Tab: "other",
	})
	if err != nil {
		t.Fatalf("Execute zero time failed: %v", err)
	}
}
