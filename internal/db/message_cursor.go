package db

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type messageListCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ATURI     string    `json:"at_uri"`
}

func encodeMessageListCursor(cursor messageListCursor) string {
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ATURI) == "" {
		return ""
	}
	payload, err := json.Marshal(struct {
		CreatedAt string `json:"created_at"`
		ATURI     string `json:"at_uri"`
	}{
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ATURI:     cursor.ATURI,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeMessageListCursor(encoded string) (messageListCursor, bool) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return messageListCursor{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return messageListCursor{}, false
	}
	var raw struct {
		CreatedAt string `json:"created_at"`
		ATURI     string `json:"at_uri"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return messageListCursor{}, false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw.CreatedAt))
	if err != nil {
		return messageListCursor{}, false
	}
	if strings.TrimSpace(raw.ATURI) == "" {
		return messageListCursor{}, false
	}
	return messageListCursor{
		CreatedAt: createdAt,
		ATURI:     strings.TrimSpace(raw.ATURI),
	}, true
}

func messageKeysetClause(sort, direction string, cursor messageListCursor) (string, []interface{}, bool) {
	desc := sort != "oldest"

	switch {
	case direction == "prev" && desc:
		return `(created_at > ? OR (created_at = ? AND at_uri > ?))`, []interface{}{cursor.CreatedAt, cursor.CreatedAt, cursor.ATURI}, true
	case direction == "prev" && !desc:
		return `(created_at < ? OR (created_at = ? AND at_uri < ?))`, []interface{}{cursor.CreatedAt, cursor.CreatedAt, cursor.ATURI}, true
	case direction != "prev" && desc:
		return `(created_at < ? OR (created_at = ? AND at_uri < ?))`, []interface{}{cursor.CreatedAt, cursor.CreatedAt, cursor.ATURI}, false
	default:
		return `(created_at > ? OR (created_at = ? AND at_uri > ?))`, []interface{}{cursor.CreatedAt, cursor.CreatedAt, cursor.ATURI}, false
	}
}

func messageKeysetOrder(sort string, reverse bool) string {
	desc := sort != "oldest"
	if reverse {
		desc = !desc
	}
	if desc {
		return "created_at DESC, at_uri DESC"
	}
	return "created_at ASC, at_uri ASC"
}

func reverseMessages(messages []Message) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}
