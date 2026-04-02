package xrpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Client     *http.Client
	Auth       *AuthInfo
	AdminToken *string
	Host       string
	UserAgent  *string
	Headers    map[string]string
}

type AuthInfo struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	Handle     string `json:"handle"`
	Did        string `json:"did"`
}

type XRPCError struct {
	ErrStr  string `json:"error"`
	Message string `json:"message"`
}

func (xe *XRPCError) Error() string {
	if xe == nil {
		return "unknown xrpc error"
	}
	if xe.Message == "" {
		return xe.ErrStr
	}
	return fmt.Sprintf("%s: %s", xe.ErrStr, xe.Message)
}

type RatelimitInfo struct {
	Limit     int
	Remaining int
	Policy    string
	Reset     time.Time
}

type Error struct {
	StatusCode int
	Wrapped    error
	Ratelimit  *RatelimitInfo
}

func (e *Error) Error() string {
	if e == nil {
		return "unknown xrpc error"
	}
	if e.Wrapped == nil {
		return fmt.Sprintf("XRPC ERROR %d", e.StatusCode)
	}
	if e.StatusCode == http.StatusTooManyRequests && e.Ratelimit != nil {
		return fmt.Sprintf("XRPC ERROR %d: %s (throttled until %s)", e.StatusCode, e.Wrapped, e.Ratelimit.Reset.Local())
	}
	return fmt.Sprintf("XRPC ERROR %d: %s", e.StatusCode, e.Wrapped)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Wrapped
}

func (c *Client) LexDo(ctx context.Context, method string, inputEncoding string, endpoint string, params map[string]any, bodyData any, out any) error {
	var httpMethod string
	switch method {
	case "query":
		httpMethod = http.MethodGet
	case "procedure":
		httpMethod = http.MethodPost
	default:
		return fmt.Errorf("unsupported xrpc method kind %q", method)
	}

	var body io.Reader
	if bodyData != nil {
		if reader, ok := bodyData.(io.Reader); ok {
			body = reader
		} else {
			encoded, err := json.Marshal(bodyData)
			if err != nil {
				return fmt.Errorf("marshal xrpc request: %w", err)
			}
			body = bytes.NewReader(encoded)
		}
	}

	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.Host), "/") + "/xrpc/" + endpoint)
	if err != nil {
		return fmt.Errorf("build xrpc url: %w", err)
	}
	if len(params) > 0 {
		query := u.Query()
		for key, value := range params {
			switch typed := value.(type) {
			case []string:
				for _, item := range typed {
					query.Add(key, item)
				}
			default:
				query.Add(key, fmt.Sprint(value))
			}
		}
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, u.String(), body)
	if err != nil {
		return fmt.Errorf("build xrpc request: %w", err)
	}
	if bodyData != nil && strings.TrimSpace(inputEncoding) != "" {
		req.Header.Set("Content-Type", inputEncoding)
	}
	if c.UserAgent != nil && strings.TrimSpace(*c.UserAgent) != "" {
		req.Header.Set("User-Agent", *c.UserAgent)
	} else {
		req.Header.Set("User-Agent", "mr-valinskys-adequate-bridge/atproto")
	}
	for key, value := range c.Headers {
		req.Header.Set(key, value)
	}

	if c.AdminToken != nil && strings.HasPrefix(endpoint, "com.atproto.admin.") {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+*c.AdminToken)))
	} else if c.Auth != nil && strings.TrimSpace(c.Auth.AccessJwt) != "" {
		req.Header.Set("Authorization", "Bearer "+c.Auth.AccessJwt)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var xerr XRPCError
		if err := json.NewDecoder(resp.Body).Decode(&xerr); err != nil {
			return errorFromHTTPResponse(resp, fmt.Errorf("failed to decode xrpc error message: %w", err))
		}
		return errorFromHTTPResponse(resp, &xerr)
	}

	if out == nil {
		return nil
	}
	if buffer, ok := out.(*bytes.Buffer); ok {
		if _, err := io.Copy(buffer, resp.Body); err != nil {
			return fmt.Errorf("read xrpc response: %w", err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode xrpc response: %w", err)
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func errorFromHTTPResponse(resp *http.Response, wrapped error) error {
	result := &Error{
		StatusCode: resp.StatusCode,
		Wrapped:    wrapped,
	}
	if resp.Header.Get("ratelimit-limit") == "" {
		return result
	}

	result.Ratelimit = &RatelimitInfo{
		Policy: resp.Header.Get("ratelimit-policy"),
	}
	if n, err := strconv.ParseInt(resp.Header.Get("ratelimit-reset"), 10, 64); err == nil {
		result.Ratelimit.Reset = time.Unix(n, 0)
	}
	if n, err := strconv.ParseInt(resp.Header.Get("ratelimit-limit"), 10, 64); err == nil {
		result.Ratelimit.Limit = int(n)
	}
	if n, err := strconv.ParseInt(resp.Header.Get("ratelimit-remaining"), 10, 64); err == nil {
		result.Ratelimit.Remaining = int(n)
	}
	return result
}
