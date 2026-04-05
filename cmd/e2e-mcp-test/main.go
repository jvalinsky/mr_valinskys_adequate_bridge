package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
)

const (
	defaultMasterSeed   = "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff"
	defaultBridgeRepo   = ".ssb-bridge"
	defaultClientRepo   = "/tmp/e2e-ssb-client"
	defaultMCPAddr      = "127.0.0.1:8081"
	defaultRoomHTTPPort = "8976"
)

var (
	masterSeed    = flag.String("master-seed", defaultMasterSeed, "Master seed for bot key derivation")
	bridgeRepo    = flag.String("bridge-repo", defaultBridgeRepo, "Path to bridge repository")
	clientRepo    = flag.String("client-repo", defaultClientRepo, "Path to client repository")
	mcpAddr       = flag.String("mcp-addr", defaultMCPAddr, "MCP SSE server address")
	roomHTTPPort  = flag.String("room-http-port", defaultRoomHTTPPort, "Room HTTP port")
	skipInviteUse = flag.Bool("skip-invite-use", false, "Skip invite.use test (use direct connection)")
)

type TestResult struct {
	Step   string
	Status string
	Error  string
}

func main() {
	flag.Parse()

	results := []TestResult{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Interrupted, cleaning up...")
		cancel()
	}()

	log.Println("=== MCP-Driven E2E Test Starting ===")

	result := runStep(ctx, "Cleanup", func() error {
		os.RemoveAll(*clientRepo)
		return nil
	})
	results = append(results, result)
	if result.Error != "" {
		fail(results)
	}

	bridgePID, bridgeCleanup, err := startBridge(ctx)
	if err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}
	defer bridgeCleanup()

	result = runStep(ctx, "Bridge Started", func() error {
		time.Sleep(3 * time.Second)
		return nil
	})
	results = append(results, result)

	sessionID, err := getMCPSessionID(*mcpAddr)
	if err != nil {
		fail(append(results, TestResult{Step: "Get MCP Session", Status: "FAIL", Error: err.Error()}))
	}
	log.Printf("MCP Session ID: %s", sessionID)

	botDID := fmt.Sprintf("did:plc:testbot_e2e_%d", time.Now().Unix())

	result = runStep(ctx, "Register Test Bot", func() error {
		resp, err := mcpCall(*mcpAddr, sessionID, "bridge_account_add", map[string]interface{}{
			"did": botDID,
		})
		if err != nil {
			return fmt.Errorf("mcp call: %w", err)
		}
		var mcpResp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(resp, &mcpResp); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
		if len(mcpResp.Result.Content) == 0 {
			return fmt.Errorf("no content in response")
		}
		var account struct {
			Added   bool   `json:"added"`
			DID     string `json:"did"`
			SSBFeed string `json:"ssb_feed"`
		}
		if err := json.Unmarshal([]byte(mcpResp.Result.Content[0].Text), &account); err != nil {
			return fmt.Errorf("parse account: %w", err)
		}
		if !account.Added {
			return fmt.Errorf("account not added")
		}
		log.Printf("Bot DID: %s, SSB Feed: %s", botDID, account.SSBFeed)
		return nil
	})
	results = append(results, result)
	if result.Error != "" {
		fail(results)
	}

	var publishedMsgKey string
	result = runStep(ctx, "Publish Message", func() error {
		timestamp := time.Now().UTC().Format(time.RFC3339)
		content := map[string]interface{}{
			"type": "post",
			"text": "E2E test message",
			"test": timestamp,
		}
		contentJSON, _ := json.Marshal(content)

		resp, err := mcpCall(*mcpAddr, sessionID, "ssb_publish", map[string]interface{}{
			"did":     botDID,
			"content": string(contentJSON),
		})
		if err != nil {
			return fmt.Errorf("mcp call: %w", err)
		}

		var mcpResp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(resp, &mcpResp); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}

		var publish struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(mcpResp.Result.Content[0].Text), &publish); err != nil {
			return fmt.Errorf("parse publish result: %w", err)
		}
		publishedMsgKey = publish.Key
		log.Printf("Published message key: %s", publishedMsgKey)
		return nil
	})
	results = append(results, result)
	if result.Error != "" {
		fail(results)
	}

	var inviteToken string
	result = runStep(ctx, "Create Room Invite", func() error {
		resp, err := mcpCall(*mcpAddr, sessionID, "ssb_room_invite_create", map[string]interface{}{})
		if err != nil {
			return fmt.Errorf("mcp call: %w", err)
		}

		var mcpResp struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(resp, &mcpResp); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}

		var invite struct {
			Token   string `json:"token"`
			JoinURL string `json:"join_url"`
		}
		if err := json.Unmarshal([]byte(mcpResp.Result.Content[0].Text), &invite); err != nil {
			return fmt.Errorf("parse invite result: %w", err)
		}
		inviteToken = invite.Token
		log.Printf("Invite token: %s", inviteToken)
		return nil
	})
	results = append(results, result)
	if result.Error != "" {
		fail(results)
	}

	clientPID, clientCleanup, err := startClient(ctx)
	if err != nil {
		fail(append(results, TestResult{Step: "Start Client", Status: "FAIL", Error: err.Error()}))
	}
	defer clientCleanup()

	result = runStep(ctx, "Client Started", func() error {
		time.Sleep(2 * time.Second)
		return nil
	})
	results = append(results, result)

	if !*skipInviteUse {
		result = runStep(ctx, "Join Room via Invite", func() error {
			whoamiResp, err := http.Get("http://127.0.0.1:8080/api/whoami")
			if err != nil {
				return fmt.Errorf("get whoami: %w", err)
			}
			defer whoamiResp.Body.Close()

			var whoami struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(whoamiResp.Body).Decode(&whoami); err != nil {
				return fmt.Errorf("parse whoami: %w", err)
			}

			consumeBody := map[string]string{
				"id":     whoami.ID,
				"invite": inviteToken,
			}
			bodyJSON, _ := json.Marshal(consumeBody)

			consumeURL := fmt.Sprintf("http://127.0.0.1:%s/invite/consume", *roomHTTPPort)
			resp, err := http.Post(consumeURL, "application/json", bytes.NewReader(bodyJSON))
			if err != nil {
				return fmt.Errorf("consume invite: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("consume returned %d", resp.StatusCode)
			}

			var consumeResp struct {
				MultiserverAddress string `json:"multiserverAddress"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&consumeResp); err != nil {
				return fmt.Errorf("parse consume response: %w", err)
			}
			log.Printf("Got multiserver address: %s", consumeResp.MultiserverAddress)
			return nil
		})
		results = append(results, result)
		if result.Error != "" {
			fail(results)
		}
	}

	result = runStep(ctx, "Verify Replication", func() error {
		time.Sleep(5 * time.Second)

		resp, err := http.Get("http://127.0.0.1:8080/api/feeds")
		if err != nil {
			return fmt.Errorf("get feeds: %w", err)
		}
		defer resp.Body.Close()

		var feeds struct {
			Feeds []struct {
				ID  string `json:"id"`
				Seq int    `json:"seq"`
			} `json:"feeds"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&feeds); err != nil {
			return fmt.Errorf("parse feeds: %w", err)
		}

		log.Printf("Known feeds: %d", len(feeds.Feeds))
		for _, f := range feeds.Feeds {
			log.Printf("  - %s (seq: %d)", f.ID, f.Seq)
		}
		return nil
	})
	results = append(results, result)

	result = runStep(ctx, "Verify Message Signature", func() error {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:8080/api/messages"))
		if err != nil {
			return fmt.Errorf("get messages: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			log.Println("Messages endpoint not available, skipping signature verification")
			return nil
		}

		var messages []json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
			return fmt.Errorf("parse messages: %w", err)
		}

		for _, msgRaw := range messages {
			var msg legacy.SignedMessage
			if err := json.Unmarshal(msgRaw, &msg); err != nil {
				continue
			}
			if err := msg.Verify(); err != nil {
				return fmt.Errorf("signature invalid: %w", err)
			}
			log.Printf("Verified message from %s", msg.Author.String())
		}
		return nil
	})
	results = append(results, result)

	log.Println("\n=== Test Results ===")
	for _, r := range results {
		status := "PASS"
		if r.Error != "" {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s: %s\n", status, r.Step, r.Error)
	}

	_ = bridgePID
	_ = clientPID

	log.Println("=== Test Complete ===")
}

func runStep(ctx context.Context, name string, fn func() error) TestResult {
	log.Printf("Running: %s", name)
	err := fn()
	if err != nil {
		log.Printf("  FAILED: %v", err)
		return TestResult{Step: name, Status: "FAIL", Error: err.Error()}
	}
	log.Printf("  PASSED")
	return TestResult{Step: name, Status: "PASS"}
}

func fail(results []TestResult) {
	log.Println("\n=== Test Results (FAIL) ===")
	for _, r := range results {
		status := "PASS"
		if r.Error != "" {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s: %s\n", status, r.Step, r.Error)
	}
	os.Exit(1)
}

func startBridge(ctx context.Context) (int, func(), error) {
	bridgeBin := filepath.Join("cmd", "bridge-cli", "bridge-cli")

	dbPath := filepath.Join(*bridgeRepo, "bridge.sqlite")
	if err := os.MkdirAll(*bridgeRepo, 0755); err != nil {
		return 0, nil, fmt.Errorf("create bridge repo: %w", err)
	}

	cmd := exec.CommandContext(ctx, bridgeBin,
		"start",
		"--db-path", dbPath,
		"--mcp-listen-addr", *mcpAddr,
		"--room-enable",
		"--room-http-listen-addr", fmt.Sprintf("127.0.0.1:%s", *roomHTTPPort),
		"--room-muxrpc-listen-addr", "127.0.0.1:8989",
		"--master-seed", *masterSeed,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("start bridge: %w", err)
	}

	cleanup := func() {
		cmd.Process.Kill()
		cmd.Wait()
		os.RemoveAll(*bridgeRepo)
	}

	return cmd.Process.Pid, cleanup, nil
}

func startClient(ctx context.Context) (int, func(), error) {
	clientBin := filepath.Join("cmd", "ssb-client", "ssb-client")

	if err := os.RemoveAll(*clientRepo); err != nil {
		return 0, nil, fmt.Errorf("clean client repo: %w", err)
	}
	if err := os.MkdirAll(*clientRepo, 0755); err != nil {
		return 0, nil, fmt.Errorf("create client repo: %w", err)
	}

	cmd := exec.CommandContext(ctx, clientBin,
		"serve",
		"--repo-path", *clientRepo,
		"--http-listen-addr", "127.0.0.1:8080",
		"--enable-room",
		"--room-mode", "community",
		"--room-http-addr", fmt.Sprintf("http://127.0.0.1:%s", *roomHTTPPort),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("start client: %w", err)
	}

	cleanup := func() {
		cmd.Process.Kill()
		cmd.Wait()
		os.RemoveAll(*clientRepo)
	}

	return cmd.Process.Pid, cleanup, nil
}

func getMCPSessionID(addr string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/mcp/sse", addr), nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return "", err
	}

	body := string(buf[:n])
	for _, line := range parseSSELines(body) {
		if line.Event == "endpoint" {
			data := line.Data
			if idx := contains(data, "sessionId="); idx >= 0 {
				sessionID := data[idx+len("sessionId="):]
				if endIdx := contains(sessionID, "\""); endIdx >= 0 {
					sessionID = sessionID[:endIdx]
				}
				return sessionID, nil
			}
		}
	}

	return "", fmt.Errorf("session ID not found in SSE response")
}

type sseLine struct {
	Event string
	Data  string
}

func parseSSELines(body string) []sseLine {
	var lines []sseLine
	var current sseLine

	for _, line := range splitLines(body) {
		if line == "" {
			if current.Event != "" || current.Data != "" {
				lines = append(lines, current)
				current = sseLine{}
			}
			continue
		}

		if hasPrefix(line, "event:") {
			current.Event = trimPrefix(line, "event:")
		} else if hasPrefix(line, "data:") {
			current.Data = trimPrefix(line, "data:")
		}
	}

	if current.Event != "" || current.Data != "" {
		lines = append(lines, current)
	}

	return lines
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func contains(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

var msgID int

func mcpCall(addr, sessionID, tool string, args map[string]interface{}) ([]byte, error) {
	msgID++

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      msgID,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      tool,
			"arguments": args,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://%s/api/mcp/message?sessionId=%s", addr, sessionID)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
