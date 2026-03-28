package presentation

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
)

// DetailField is one labeled summary value rendered in UI detail views.
type DetailField struct {
	Label string
	Value string
}

// PrettyJSON formats JSON for display and falls back to the raw string when it
// cannot be decoded.
func PrettyJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(empty)"
	}

	var decoded interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}

	formatted, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return raw
	}
	return string(formatted)
}

// SummarizeATProtoMessage extracts a short human-readable summary from a stored
// ATProto record payload.
func SummarizeATProtoMessage(message db.Message) []DetailField {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(message.RawATJson), &payload); err != nil {
		return fallbackSummaryFields(message.RawATJson, "ATProto")
	}

	collector := newDetailCollector()
	collector.add("Collection", message.Type)
	collector.add("Text", stringAt(payload, "text"))
	collector.add("Operation", stringAt(payload, "op"))
	collector.add("Created At", stringAt(payload, "createdAt"))
	collector.add("Subject URI", nestedString(payload, "subject", "uri"))
	collector.add("Subject URI", stringAt(payload, "subject"))
	collector.add("Reply Root", nestedString(payload, "reply", "root", "uri"))
	collector.add("Reply Parent", nestedString(payload, "reply", "parent", "uri"))
	collector.add("Embed Type", nestedString(payload, "embed", "$type"))
	collector.add("Languages", joinArray(payload["langs"]))
	collector.add("Sequence", jsonScalar(payload["seq"]))
	if len(collector.fields) > 0 {
		return collector.fields
	}
	return fallbackSummaryFields(message.RawATJson, "ATProto")
}

// SummarizeSSBMessage extracts a short human-readable summary from a stored SSB
// bridged message payload.
func SummarizeSSBMessage(message db.Message) []DetailField {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(message.RawSSBJson), &payload); err != nil {
		return fallbackSummaryFields(message.RawSSBJson, "SSB")
	}

	collector := newDetailCollector()
	collector.add("Type", stringAt(payload, "type"))
	collector.add("Text", stringAt(payload, "text"))
	collector.add("Subject", stringAt(payload, "_atproto_subject"))
	collector.add("Contact DID", stringAt(payload, "_atproto_contact"))
	collector.add("Reply Root", stringAt(payload, "_atproto_reply_root"))
	collector.add("Reply Parent", stringAt(payload, "_atproto_reply_parent"))
	collector.add("Root", stringAt(payload, "root"))
	collector.add("Branch", stringAt(payload, "branch"))
	collector.add("Following", jsonScalar(payload["following"]))
	collector.add("Vote", voteSummary(payload["vote"]))
	if len(collector.fields) > 0 {
		return collector.fields
	}
	return fallbackSummaryFields(message.RawSSBJson, "SSB")
}

func fallbackSummaryFields(raw string, label string) []DetailField {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return []DetailField{{
		Label: label + " Payload",
		Value: raw,
	}}
}

type detailCollector struct {
	fields []DetailField
	seen   map[string]struct{}
}

func newDetailCollector() *detailCollector {
	return &detailCollector{seen: make(map[string]struct{})}
}

func (c *detailCollector) add(label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	key := label + "\x00" + value
	if _, ok := c.seen[key]; ok {
		return
	}
	c.seen[key] = struct{}{}
	c.fields = append(c.fields, DetailField{
		Label: label,
		Value: value,
	})
}

func stringAt(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	return jsonScalar(m[key])
}

func nestedString(m map[string]interface{}, path ...string) string {
	var current interface{} = m
	for _, key := range path {
		next, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = next[key]
	}
	return jsonScalar(current)
}

func jsonScalar(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case json.Number:
		return val.String()
	default:
		return ""
	}
}

func joinArray(v interface{}) string {
	items, ok := v.([]interface{})
	if !ok || len(items) == 0 {
		return ""
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value := jsonScalar(item); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, ", ")
}

func voteSummary(v interface{}) string {
	vote, ok := v.(map[string]interface{})
	if !ok {
		return ""
	}
	expression := strings.TrimSpace(jsonScalar(vote["expression"]))
	value := strings.TrimSpace(jsonScalar(vote["value"]))
	switch {
	case expression != "" && value != "":
		return expression + " (" + value + ")"
	case expression != "":
		return expression
	default:
		return value
	}
}
