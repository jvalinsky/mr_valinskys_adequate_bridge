package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
)

func writeJSONResponse(w http.ResponseWriter, payload interface{}) {
	writeJSONResponseWithStatus(w, http.StatusOK, payload)
}

func writeJSONResponseWithStatus(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func messageDTO(msg feedlog.StoredMessage) map[string]interface{} {
	return map[string]interface{}{
		"author":    msg.Metadata.Author,
		"sequence":  msg.Metadata.Sequence,
		"timestamp": msg.Metadata.Timestamp,
		"key":       msg.Key,
		"content":   json.RawMessage(msg.Value),
	}
}

func messageDetailDTO(msg *feedlog.StoredMessage) map[string]interface{} {
	dto := messageDTO(*msg)
	dto["previous"] = msg.Metadata.Previous
	dto["hash"] = msg.Metadata.Hash
	dto["received"] = msg.Received
	return dto
}

func replicationTargetFromContact(content map[string]interface{}) (string, bool) {
	if len(content) == 0 {
		return "", false
	}

	msgType, _ := content["type"].(string)
	if strings.TrimSpace(msgType) != "contact" {
		return "", false
	}

	contact, _ := content["contact"].(string)
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return "", false
	}
	if !strings.HasPrefix(contact, "@") {
		contact = "@" + contact
	}

	following := boolValueOrDefault(content["following"], true)
	blocking := boolValueOrDefault(content["blocking"], false)
	if !following || blocking {
		return "", false
	}

	return contact, true
}

func boolValueOrDefault(value interface{}, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case float64:
		return v != 0
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0
		}
		if f, err := v.Float64(); err == nil {
			return f != 0
		}
	}
	return defaultValue
}

func decodePeerPublicKey(raw string) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "@")
	trimmed = strings.TrimSuffix(trimmed, ".ed25519")
	if trimmed == "" {
		return nil, fmt.Errorf("public key is required")
	}
	key, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length %d", len(key))
	}
	return ed25519.PublicKey(key), nil
}

func parseMultiserverConnectAddress(raw string) (string, ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, fmt.Errorf("multiserver address is required")
	}

	noPrefix := strings.TrimPrefix(trimmed, "net:")
	parts := strings.Split(noPrefix, "~shs:")
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid multiserver format; expected net:host:port~shs:pubkey")
	}

	hostPort := strings.TrimSpace(parts[0])
	if hostPort == "" {
		return "", nil, fmt.Errorf("multiserver host:port is required")
	}

	pubkey, err := decodePeerPublicKey(parts[1])
	if err != nil {
		return "", nil, err
	}
	return hostPort, pubkey, nil
}

func resolvePeerConnectTarget(multiserver, address, pubkey string) (string, ed25519.PublicKey, error) {
	joined := strings.TrimSpace(multiserver)
	if joined == "" {
		joined = strings.TrimSpace(address)
	}

	if strings.Contains(joined, "~shs:") || strings.HasPrefix(joined, "net:") {
		return parseMultiserverConnectAddress(joined)
	}

	if joined == "" {
		return "", nil, fmt.Errorf("address is required")
	}

	key, err := decodePeerPublicKey(pubkey)
	if err != nil {
		return "", nil, err
	}
	return joined, key, nil
}
