package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (h *clientUIHandler) ensureIndexSynced() error {
	if h == nil || h.index == nil {
		return nil
	}
	whoami, _ := h.sbot.Whoami()
	return h.index.sync(h.sbot.Store(), whoami, h.keyPair)
}

func (h *clientUIHandler) writeJSONError(w http.ResponseWriter, status int, err error) {
	if err == nil {
		err = http.ErrAbortHandler
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": strings.TrimSpace(err.Error()),
	})
}
