package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
)

func TestRoomInviteUseE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	roomRepo := filepath.Join(tmpDir, "room-repo")

	roomLogger := log.New(io.Discard, "", 0)
	roomRT, err := room.Start(ctx, room.Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       roomRepo,
		Mode:           "open",
	}, roomLogger)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start room runtime: %v", err)
	}
	defer roomRT.Close()

	roomHTTPAddr := roomRT.HTTPAddr()
	httpClient := &http.Client{Timeout: 5 * time.Second}

	createInviteReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+roomHTTPAddr+"/create-invite", nil)
	if err != nil {
		t.Fatalf("create invite request: %v", err)
	}
	createInviteReq.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(createInviteReq)
	if err != nil {
		t.Fatalf("create invite request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create invite expected 200 got %d: %s", resp.StatusCode, string(body))
	}

	var invitePayload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&invitePayload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	inviteURL := invitePayload["url"]
	if inviteURL == "" || !strings.Contains(inviteURL, "/join?token=") {
		t.Fatalf("invalid invite URL: %s", inviteURL)
	}

	inviteToken := extractToken(inviteURL)
	if inviteToken == "" {
		t.Fatalf("failed to extract token from: %s", inviteURL)
	}

	t.Logf("Created invite: %s", inviteURL)
	t.Logf("Extracted token: %s", inviteToken)

	if !strings.Contains(inviteURL, roomHTTPAddr) {
		t.Fatalf("invite URL should contain room HTTP address: %s", roomHTTPAddr)
	}

	testKeys, _ := keys.Generate()
	testFeedID := testKeys.FeedRef().String()

	if _, err := consumeInviteHTTP(ctx, httpClient, roomHTTPAddr, testFeedID, inviteToken); err != nil {
		t.Fatalf("consume invite failed: %v", err)
	}

	t.Logf("E2E test completed successfully - invite flow working")
}

func TestRoomInviteConsumeHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	roomRepo := filepath.Join(tmpDir, "room-repo")

	roomLogger := log.New(io.Discard, "", 0)
	roomRT, err := room.Start(ctx, room.Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       roomRepo,
		Mode:           "open",
	}, roomLogger)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start room runtime: %v", err)
	}
	defer roomRT.Close()

	roomHTTPAddr := roomRT.HTTPAddr()
	httpClient := &http.Client{Timeout: 5 * time.Second}

	createInviteReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+roomHTTPAddr+"/create-invite", nil)
	if err != nil {
		t.Fatalf("create invite request: %v", err)
	}
	createInviteReq.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(createInviteReq)
	if err != nil {
		t.Fatalf("create invite request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create invite expected 200 got %d: %s", resp.StatusCode, string(body))
	}

	var invitePayload map[string]string
	if err := json.Unmarshal(body, &invitePayload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	inviteURL := invitePayload["url"]
	if inviteURL == "" {
		t.Fatal("missing url in invite response")
	}

	inviteToken := extractToken(inviteURL)
	if inviteToken == "" {
		t.Fatal("failed to extract token from invite URL")
	}

	testKeys, _ := keys.Generate()
	testFeedID := testKeys.FeedRef().String()

	consumeResult, err := consumeInviteHTTP(ctx, httpClient, roomHTTPAddr, testFeedID, inviteToken)
	if err != nil {
		t.Fatalf("consume invite failed: %v", err)
	}

	if consumeResult.Status != "successful" {
		t.Fatalf("unexpected status: %s", consumeResult.Status)
	}

	if consumeResult.MultiserverAddress == "" {
		t.Fatal("missing multiserverAddress in consume response")
	}

	expectedPrefix := "net:"
	if !strings.HasPrefix(consumeResult.MultiserverAddress, expectedPrefix) {
		t.Fatalf("multiserverAddress should start with %s, got: %s", expectedPrefix, consumeResult.MultiserverAddress)
	}

	if !strings.Contains(consumeResult.MultiserverAddress, "~shs:") {
		t.Fatalf("multiserverAddress should contain ~shs:, got: %s", consumeResult.MultiserverAddress)
	}

	t.Logf("HTTP invite consume succeeded: %s", consumeResult.MultiserverAddress)
}

type consumeResult struct {
	Status             string `json:"status"`
	MultiserverAddress string `json:"multiserverAddress"`
}

func consumeInviteHTTP(ctx context.Context, httpClient *http.Client, roomHTTPAddr, feedID, token string) (*consumeResult, error) {
	consumeBody := map[string]string{
		"id":     feedID,
		"invite": token,
	}
	consumeJSON, err := json.Marshal(consumeBody)
	if err != nil {
		return nil, err
	}

	consumeReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+roomHTTPAddr+"/invite/consume",
		bytes.NewReader(consumeJSON))
	if err != nil {
		return nil, err
	}
	consumeReq.Header.Set("Content-Type", "application/json")

	consumeResp, err := httpClient.Do(consumeReq)
	if err != nil {
		return nil, err
	}
	defer consumeResp.Body.Close()

	consumeBodyRaw, err := io.ReadAll(consumeResp.Body)
	if err != nil {
		return nil, err
	}

	if consumeResp.StatusCode != http.StatusOK {
		return nil, &httpError{StatusCode: consumeResp.StatusCode, Body: string(consumeBodyRaw)}
	}

	var result consumeResult
	if err := json.Unmarshal(consumeBodyRaw, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type httpError struct {
	StatusCode int
	Body       string
}

func (e *httpError) Error() string {
	return "HTTP " + http.StatusText(e.StatusCode) + ": " + e.Body
}

func TestInviteTokenExtraction(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"token query param", "http://localhost:8080/join?token=abc123", "abc123"},
		{"token with ampersand", "http://localhost:8080/join?token=abc123&foo=bar", "abc123"},
		{"invite query param", "http://localhost:8080/join?invite=xyz789", "xyz789"},
		{"both params prefers token", "http://localhost:8080/join?token=abc123&invite=xyz789", "abc123"},
		{"no token", "http://localhost:8080/join", ""},
		{"https with token", "https://room.example.com/join?token=secret123&extra=data", "secret123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractToken(tt.input)
			if got != tt.expected {
				t.Errorf("extractToken(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func extractToken(inviteURL string) string {
	parsed, err := url.Parse(inviteURL)
	if err != nil {
		return ""
	}
	if token := parsed.Query().Get("token"); token != "" {
		return token
	}
	if token := parsed.Query().Get("invite"); token != "" {
		return token
	}
	return ""
}
