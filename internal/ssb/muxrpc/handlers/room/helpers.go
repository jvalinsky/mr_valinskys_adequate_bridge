package room

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

var aliasLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func parseArgList(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var args []json.RawMessage
	if err := json.Unmarshal(raw, &args); err == nil {
		return args, nil
	}

	return nil, fmt.Errorf("expected muxrpc args array")
}

func parseSingleObjectArg(raw json.RawMessage, dst interface{}) error {
	args, err := parseArgList(raw)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("expected exactly one argument")
	}
	return json.Unmarshal(args[0], dst)
}

func parseSingleStringArg(raw json.RawMessage) (string, error) {
	args, err := parseArgList(raw)
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", fmt.Errorf("expected exactly one argument")
	}
	var out string
	if err := json.Unmarshal(args[0], &out); err != nil {
		return "", err
	}
	return out, nil
}

func parseAliasRegisterArgs(raw json.RawMessage) (string, []byte, error) {
	args, err := parseArgList(raw)
	if err != nil {
		return "", nil, err
	}
	if len(args) != 2 {
		return "", nil, fmt.Errorf("expected alias and signature arguments")
	}

	var alias string
	if err := json.Unmarshal(args[0], &alias); err != nil {
		return "", nil, fmt.Errorf("parse alias: %w", err)
	}

	var signature []byte
	if err := json.Unmarshal(args[1], &signature); err != nil {
		return "", nil, fmt.Errorf("parse signature: %w", err)
	}

	return alias, signature, nil
}

func AuthenticatedFeedFromAddr(addr net.Addr) (refs.FeedRef, error) {
	return secretstream.AuthenticatedFeedFromAddr(addr)
}

func isInternalMember(s *RoomServer, ctx context.Context, feed refs.FeedRef) bool {
	if s == nil || s.members == nil {
		return false
	}
	_, err := s.members.GetByFeed(ctx, feed)
	return err == nil
}

func roomFeatures(mode roomdb.PrivacyMode) []string {
	features := []string{"tunnel", "room2", "httpInvite", "httpAuth"}
	if mode != roomdb.ModeRestricted {
		features = append(features, "alias")
	}
	return features
}

func aliasRegistrationMessage(roomID refs.FeedRef, feedID refs.FeedRef, alias string) []byte {
	return []byte("=room-alias-registration:" + roomID.String() + ":" + feedID.String() + ":" + alias)
}

func validateAliasRegistration(roomID refs.FeedRef, caller refs.FeedRef, alias string, signature []byte) error {
	alias = strings.ToLower(strings.TrimSpace(alias))
	if !aliasLabelPattern.MatchString(alias) {
		return fmt.Errorf("invalid alias")
	}
	if len(signature) == 0 {
		return fmt.Errorf("signature required")
	}
	return legacy.Signature(signature).Verify(aliasRegistrationMessage(roomID, caller, alias), caller)
}

func buildAliasURL(domain string, alias string) string {
	escaped := url.PathEscape(alias)
	base := normalizeAliasBaseURL(domain)
	if base == "" {
		return "/" + escaped
	}
	return base + "/" + escaped
}

func normalizeAliasBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}

	host := trimmed
	if parsedHost, _, err := net.SplitHostPort(trimmed); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return "http://" + trimmed
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "http://" + trimmed
	}
	return "https://" + trimmed
}
