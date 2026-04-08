package main

import (
	"encoding/json"
	"net/http"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
)

func writeJSONResponse(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
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
