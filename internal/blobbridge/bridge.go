package blobbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"go.cryptoscope.co/ssb"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
)

type Bridge struct {
	db        *db.DB
	blobStore ssb.BlobStore
	xrpc      lexutil.LexClient
	logger    *log.Logger
}

func New(database *db.DB, blobStore ssb.BlobStore, xrpcClient lexutil.LexClient, logger *log.Logger) *Bridge {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Bridge{
		db:        database,
		blobStore: blobStore,
		xrpc:      xrpcClient,
		logger:    logger,
	}
}

func (b *Bridge) BridgeRecordBlobs(ctx context.Context, atDID string, mapped map[string]interface{}, rawRecordJSON []byte) error {
	candidates, err := collectBlobCandidates(mapped, rawRecordJSON)
	if err != nil {
		return fmt.Errorf("collect blob candidates: %w", err)
	}
	if len(candidates) == 0 {
		delete(mapped, "_atproto_avatar_cid")
		return nil
	}

	var refs []string
	for _, cand := range candidates {
		blobRef, err := b.ensureBlob(ctx, atDID, cand)
		if err != nil {
			return err
		}
		refs = append(refs, blobRef)

		if avatarCID, ok := mapped["_atproto_avatar_cid"].(string); ok && avatarCID == cand.CID {
			mapped["image"] = blobRef
		}
	}

	delete(mapped, "_atproto_avatar_cid")
	if len(refs) > 0 {
		mapped["blob_refs"] = refs
	}
	return nil
}

type blobCandidate struct {
	CID      string
	MimeType string
}

func collectBlobCandidates(mapped map[string]interface{}, rawRecordJSON []byte) ([]blobCandidate, error) {
	candidates := make([]blobCandidate, 0, 4)
	seen := make(map[string]struct{})

	add := func(cid, mimeType string) {
		if cid == "" {
			return
		}
		if _, ok := seen[cid]; ok {
			return
		}
		seen[cid] = struct{}{}
		candidates = append(candidates, blobCandidate{CID: cid, MimeType: mimeType})
	}

	if avatarCID, ok := mapped["_atproto_avatar_cid"].(string); ok {
		add(avatarCID, "")
	}

	var raw any
	if len(rawRecordJSON) > 0 {
		if err := json.Unmarshal(rawRecordJSON, &raw); err != nil {
			return nil, err
		}
		walkAny(raw, func(v map[string]any) {
			typ, _ := v["$type"].(string)
			if typ != "blob" {
				return
			}
			add(extractBlobCID(v), extractBlobMIME(v))
		})
	}

	return candidates, nil
}

func extractBlobCID(v map[string]any) string {
	if ref, ok := v["ref"]; ok {
		switch t := ref.(type) {
		case map[string]any:
			if link, ok := t["$link"].(string); ok {
				return link
			}
		case string:
			return t
		}
	}

	if cid, ok := v["cid"].(string); ok {
		return cid
	}
	return ""
}

func extractBlobMIME(v map[string]any) string {
	if mime, ok := v["mimeType"].(string); ok {
		return mime
	}
	return ""
}

func walkAny(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, next := range t {
			walkAny(next, fn)
		}
	case []any:
		for _, next := range t {
			walkAny(next, fn)
		}
	}
}

func (b *Bridge) ensureBlob(ctx context.Context, atDID string, cand blobCandidate) (string, error) {
	existing, err := b.db.GetBlob(ctx, cand.CID)
	if err != nil {
		return "", fmt.Errorf("query blob mapping for %s: %w", cand.CID, err)
	}
	if existing != nil && existing.SSBBlobRef != "" {
		return existing.SSBBlobRef, nil
	}

	if b.xrpc == nil {
		return "", fmt.Errorf("blob fetch unavailable for %s: no xrpc client configured", cand.CID)
	}

	payload, err := atproto.SyncGetBlob(ctx, b.xrpc, cand.CID, atDID)
	if err != nil {
		return "", fmt.Errorf("fetch blob cid=%s did=%s: %w", cand.CID, atDID, err)
	}

	blobRef, err := b.blobStore.Put(bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("store blob cid=%s: %w", cand.CID, err)
	}

	if err := b.db.AddBlob(ctx, db.Blob{
		ATCID:      cand.CID,
		SSBBlobRef: blobRef.Ref(),
		Size:       int64(len(payload)),
		MimeType:   cand.MimeType,
	}); err != nil {
		return "", fmt.Errorf("persist blob mapping cid=%s: %w", cand.CID, err)
	}

	b.logger.Printf("event=blob_bridged did=%s cid=%s ssb_blob_ref=%s size=%d mime=%s", atDID, cand.CID, blobRef.Ref(), len(payload), strings.TrimSpace(cand.MimeType))
	return blobRef.Ref(), nil
}
