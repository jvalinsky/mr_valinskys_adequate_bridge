package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
)

type InviteUseHandler struct {
	roomHTTPAddr string
}

func NewInviteUseHandler(roomHTTPAddr string) *InviteUseHandler {
	return &InviteUseHandler{
		roomHTTPAddr: strings.TrimSuffix(roomHTTPAddr, "/"),
	}
}

func (h *InviteUseHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 2 && m[0] == "invite" && m[1] == "use"
}

func (h *InviteUseHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("invite.use is async"))
		return
	}

	inviteCode, err := parseSingleStringArg(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: parse args: %w", err))
		return
	}

	caller, err := room.AuthenticatedFeedFromAddr(req.RemoteAddr())
	if err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: get caller: %w", err))
		return
	}

	token, consumeURL, err := h.extractTokenAndURL(inviteCode)
	if err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: parse invite: %w", err))
		return
	}

	if consumeURL == "" {
		consumeURL = h.roomHTTPAddr + "/invite/consume"
	}

	body := map[string]string{
		"id":     caller.String(),
		"invite": token,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: marshal body: %w", err))
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, consumeURL, bytes.NewReader(bodyJSON))
	if err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: create request: %w", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: http request: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			req.CloseWithError(fmt.Errorf("invite.use: %s", errResp.Error))
			return
		}
		req.CloseWithError(fmt.Errorf("invite.use: server returned %d", resp.StatusCode))
		return
	}

	var result struct {
		Status             string `json:"status"`
		MultiserverAddress string `json:"multiserverAddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		req.CloseWithError(fmt.Errorf("invite.use: decode response: %w", err))
		return
	}

	req.Return(ctx, map[string]interface{}{
		"multiserverAddress": result.MultiserverAddress,
	})
}

func (h *InviteUseHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *InviteUseHandler) extractTokenAndURL(inviteCode string) (token string, consumeURL string, err error) {
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" {
		return "", "", fmt.Errorf("empty invite code")
	}

	parsedURL, err := url.Parse(inviteCode)
	if err != nil {
		return "", "", fmt.Errorf("invalid invite URL: %w", err)
	}

	if parsedURL.Host == "" {
		return inviteCode, "", nil
	}

	if parsedURL.Query().Get("token") != "" {
		token = parsedURL.Query().Get("token")
	} else if parsedURL.Query().Get("invite") != "" {
		token = parsedURL.Query().Get("invite")
	} else {
		return "", "", fmt.Errorf("no token found in invite URL")
	}

	if strings.Contains(parsedURL.Path, "/invite/consume") {
		consumeURL = parsedURL.Scheme + "://" + parsedURL.Host + "/invite/consume"
	}

	return token, consumeURL, nil
}

func parseSingleStringArg(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("no arguments")
	}

	var args []json.RawMessage
	if err := json.Unmarshal(raw, &args); err == nil && len(args) > 0 {
		var s string
		if err := json.Unmarshal(args[0], &s); err != nil {
			return "", err
		}
		return s, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}
