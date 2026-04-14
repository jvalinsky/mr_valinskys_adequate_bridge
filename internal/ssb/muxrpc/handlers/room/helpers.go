package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

func isInternalMember(s *RoomServer, ctx context.Context, feed refs.FeedRef) (bool, error) {
	if s == nil || s.members == nil {
		return false, fmt.Errorf("room members service unavailable")
	}
	_, err := s.members.GetByFeed(ctx, feed)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func ensureMemberID(ctx context.Context, members roomdb.MembersService, feed refs.FeedRef, role roomdb.Role) (int64, error) {
	if members == nil {
		return 0, fmt.Errorf("room members service unavailable")
	}

	member, err := members.GetByFeed(ctx, feed)
	if err == nil {
		return member.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	memberID, addErr := members.Add(ctx, feed, role)
	if addErr == nil {
		return memberID, nil
	}

	// Handle races on unique pub_key by re-reading the row after a failed insert.
	member, lookupErr := members.GetByFeed(ctx, feed)
	if lookupErr == nil {
		return member.ID, nil
	}

	return 0, fmt.Errorf("add member: %w", addErr)
}

func observeMembershipLookupFailure(server *RoomServer, method string, err error) {
	errorKind := roomErrorKind(err)
	if server != nil && server.observer != nil {
		server.observer.OnRoomMembershipLookupFailure(method, errorKind)
		server.observer.OnRoomMethodFailure(method, "membership_lookup", errorKind)
	}
	slog.Warn("room membership lookup failed", "method", method, "error_kind", errorKind, "error", err)
}

func observeRoomMethodFailure(server *RoomServer, method, reason string, err error) {
	errorKind := roomErrorKind(err)
	if server != nil && server.observer != nil {
		server.observer.OnRoomMethodFailure(method, reason, errorKind)
	}
	slog.Warn("room method failed", "method", method, "reason", reason, "error_kind", errorKind, "error", err)
}

func roomErrorKind(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "not_found"
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "sqlite_busy"),
		strings.Contains(lower, "database is locked"),
		strings.Contains(lower, "database table is locked"),
		strings.Contains(lower, "locked"):
		return "sqlite_busy"
	default:
		return "other"
	}
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
