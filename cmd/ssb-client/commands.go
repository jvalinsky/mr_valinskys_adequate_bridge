package main

import (
	"bytes"
	crypto_rand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/urfave/cli/v2"
)

func runIdentityWhoami(c *cli.Context) error {
	secretPath := filepath.Join(repoPath, "secret")
	kp, err := keys.Load(secretPath)
	if err != nil {
		return fmt.Errorf("no identity found. run 'ssb-client identity create' first: %w", err)
	}
	fmt.Printf("%s\n", kp.FeedRef().String())
	return nil
}

func runIdentityCreate(c *cli.Context) error {
	if repoPath == "" {
		repoPath = defaultRepoPath
	}
	secretPath := filepath.Join(repoPath, "secret")

	if _, err := keys.Load(secretPath); err == nil {
		return fmt.Errorf("identity already exists. remove %s to create a new one", secretPath)
	}

	kp, err := keys.Generate()
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}
	if err := keys.Save(kp, secretPath); err != nil {
		return fmt.Errorf("save keypair: %w", err)
	}

	fmt.Printf("Created new identity: %s\n", kp.FeedRef().String())
	fmt.Printf("Save the secret file at %s for backup!\n", secretPath)
	return nil
}

func runIdentityExport(c *cli.Context) error {
	secretPath := filepath.Join(repoPath, "secret")
	kp, err := keys.Load(secretPath)
	if err != nil {
		return fmt.Errorf("no identity found: %w", err)
	}

	fmt.Printf(`{
  "curve": "ed25519",
  "id": "%s",
  "private": "%s.ed25519",
  "public": "%s.ed25519"
}
`,
		kp.FeedRef().String(),
		keys.EncodePrivateKey(kp),
		keys.EncodePublicKey(kp),
	)
	return nil
}

func runIdentityImport(c *cli.Context) error {
	if repoPath == "" {
		repoPath = defaultRepoPath
	}
	secretPath := filepath.Join(repoPath, "secret")

	if _, err := keys.Load(secretPath); err == nil {
		return fmt.Errorf("identity already exists. remove %s to import a new one", secretPath)
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	kp, err := keys.ParseSecret(strings.NewReader(strings.TrimSpace(string(data))))
	if err != nil {
		return fmt.Errorf("import keypair: %w", err)
	}

	if err := keys.Save(kp, secretPath); err != nil {
		return fmt.Errorf("save keypair: %w", err)
	}

	fmt.Printf("Imported identity: %s\n", kp.FeedRef().String())
	return nil
}

// ---------------------------------------------------------------------------
// CLI client commands (hit running server's HTTP API)
// ---------------------------------------------------------------------------

func serverURL(path string) string {
	return fmt.Sprintf("http://%s%s", httpListenAddr, path)
}

func apiGet(path string) error {
	resp, err := http.Get(serverURL(path))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	// Pretty-print the JSON
	fmt.Println(prettyJSON(body))
	return nil
}

func apiPost(path string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	resp, err := http.Post(serverURL(path), "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	fmt.Println(prettyJSON(body))
	return nil
}

func runState(c *cli.Context) error {
	return apiGet("/api/state")
}

func runFeeds(c *cli.Context) error {
	return apiGet("/api/feeds")
}

func runFeed(c *cli.Context) error {
	author := c.String("author")
	limit := c.Int("limit")
	msgType := c.String("type")

	if author != "" {
		path := fmt.Sprintf("/api/feed/%s?limit=%d", url.PathEscape(author), limit)
		if msgType != "" {
			path += "&type=" + url.QueryEscape(msgType)
		}
		return apiGet(path)
	}

	path := fmt.Sprintf("/api/messages?limit=%d", limit)
	if msgType != "" {
		path += "&type=" + url.QueryEscape(msgType)
	}
	return apiGet(path)
}

func runMessage(c *cli.Context) error {
	if c.NArg() < 2 {
		return fmt.Errorf("usage: ssb-client message <feedId> <sequence>")
	}
	feedId := c.Args().Get(0)
	seq := c.Args().Get(1)
	return apiGet(fmt.Sprintf("/api/message/%s/%s", url.PathEscape(feedId), seq))
}

func runPublish(c *cli.Context) error {
	raw := c.String("raw")
	if raw != "" {
		var content map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		return apiPost("/api/publish", content)
	}

	content := map[string]interface{}{
		"type": c.String("type"),
	}
	if text := c.String("text"); text != "" {
		content["text"] = text
	}
	if contact := c.String("contact"); contact != "" {
		content["contact"] = contact
		if c.IsSet("following") {
			content["following"] = c.Bool("following")
		}
		if c.IsSet("blocking") {
			content["blocking"] = c.Bool("blocking")
		}
	}

	return apiPost("/api/publish", content)
}

func runRoomLogin(c *cli.Context) error {
	roomURL := c.Args().First()
	if roomURL == "" {
		return fmt.Errorf("missing room HTTP URL")
	}

	secretPath := filepath.Join(repoPath, "secret")
	kp, err := keys.Load(secretPath)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	cid := kp.FeedRef().String()

	ccBytes := make([]byte, 32)
	if _, err := crypto_rand.Read(ccBytes); err != nil {
		return err
	}
	cc := base64.StdEncoding.EncodeToString(ccBytes)

	// Build URL
	u, err := url.Parse(roomURL)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(u.Path, "/login") {
		u.Path = path.Join(u.Path, "login")
	}
	q := u.Query()
	q.Set("ssb-http-auth", "1")
	q.Set("cid", cid)
	q.Set("cc", cc)
	u.RawQuery = q.Encode()

	fmt.Printf("Logging in to %s...\n", u.String())

	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed: %s (body: %s)", resp.Status, string(body))
	}

	// Check for session cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "auth_token" {
			fmt.Printf("Login successful! Session cookie received: %s\n", cookie.Value)
			return nil
		}
	}

	fmt.Println("Login request sent. Check if session was established.")
	return nil
}

func runPeersList(c *cli.Context) error {
	return apiGet("/api/peers")
}

func runPeersAdd(c *cli.Context) error {
	return runPeersConnect(c)
}

func runPeersConnect(c *cli.Context) error {
	if c.NArg() < 2 {
		return fmt.Errorf("usage: ssb-client peers connect <address> <pubkey>")
	}
	return apiPost("/api/connect", map[string]string{
		"address": c.Args().Get(0),
		"pubkey":  c.Args().Get(1),
	})
}

func runReplication(c *cli.Context) error {
	return apiGet("/api/replication")
}

func runCompatProbe(c *cli.Context) error {
	resp, err := http.Get(serverURL("/api/capabilities"))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		RPC struct {
			MissingMethods []string            `json:"missingMethods"`
			ManifestByType map[string][]string `json:"manifestByType"`
		} `json:"rpc"`
		KnownGaps []string `json:"knownGaps"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode capabilities: %w", err)
	}

	if len(payload.RPC.MissingMethods) == 0 {
		fmt.Println("compat probe: OK (no required rpc methods missing)")
	} else {
		fmt.Printf("compat probe: FAIL (%d required rpc methods missing)\n", len(payload.RPC.MissingMethods))
		for _, m := range payload.RPC.MissingMethods {
			fmt.Printf(" - %s\n", m)
		}
	}

	if len(payload.KnownGaps) > 0 {
		fmt.Println("known gaps:")
		for _, gap := range payload.KnownGaps {
			fmt.Printf(" - %s\n", gap)
		}
	}

	if len(payload.RPC.MissingMethods) > 0 {
		return fmt.Errorf("compatibility requirements not met")
	}
	return nil
}
