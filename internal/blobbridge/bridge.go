// Package blobbridge mirrors ATProto blobs into the local SSB blob store.
package blobbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"go.cryptoscope.co/ssb"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
)

// Bridge fetches ATProto blobs, stores them in SSB, and persists CID mappings.
type Bridge struct {
	db        *db.DB
	blobStore ssb.BlobStore
	xrpc      lexutil.LexClient
	resolver  HostResolver
	client    *http.Client
	logger    *log.Logger
}

// HostResolver resolves the XRPC host to use for a DID-scoped blob fetch.
type HostResolver interface {
	ResolvePDSEndpoint(ctx context.Context, did string) (string, error)
}

// New constructs a blob Bridge.
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

// NewWithResolver constructs a blob Bridge that resolves the correct host per
// DID before fetching each blob.
func NewWithResolver(database *db.DB, blobStore ssb.BlobStore, resolver HostResolver, httpClient *http.Client, logger *log.Logger) *Bridge {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Bridge{
		db:        database,
		blobStore: blobStore,
		resolver:  resolver,
		client:    configuredHTTPClient(httpClient),
		logger:    logger,
	}
}

// BridgeRecordBlobs resolves blobs referenced by a mapped record into SSB-native fields.
func (b *Bridge) BridgeRecordBlobs(ctx context.Context, atDID, collection string, mapped map[string]interface{}, rawRecordJSON []byte) error {
	switch collection {
	case mapper.RecordTypePost:
		return b.bridgePostBlobs(ctx, atDID, mapped, rawRecordJSON)
	case mapper.RecordTypeProfile:
		return b.bridgeProfileBlobs(ctx, atDID, mapped, rawRecordJSON)
	default:
		return nil
	}
}

type blobCandidate struct {
	CID      string
	MimeType string
	Size     int64
	Width    int
	Height   int
	Label    string
}

func (b *Bridge) bridgePostBlobs(ctx context.Context, atDID string, mapped map[string]interface{}, rawRecordJSON []byte) error {
	var post appbsky.FeedPost
	if err := json.Unmarshal(rawRecordJSON, &post); err != nil {
		return fmt.Errorf("decode post blobs: %w", err)
	}

	candidates := postBlobCandidates(&post)
	for i, candidate := range candidates {
		blobRef, err := b.ensureBlob(ctx, atDID, candidate)
		if err != nil {
			return err
		}

		appendMention(mapped, map[string]interface{}{
			"link":   blobRef,
			"name":   candidate.Label,
			"size":   candidate.Size,
			"width":  candidate.Width,
			"height": candidate.Height,
			"type":   candidate.MimeType,
		})
		mapped["text"] = appendMarkdownBlock(asString(mapped["text"]), fmt.Sprintf("![%s](%s)", escapeMarkdownLabel(candidate.Label), blobRef))

		_ = i
	}

	return nil
}

func (b *Bridge) bridgeProfileBlobs(ctx context.Context, atDID string, mapped map[string]interface{}, rawRecordJSON []byte) error {
	var profile appbsky.ActorProfile
	if err := json.Unmarshal(rawRecordJSON, &profile); err != nil {
		return fmt.Errorf("decode profile blobs: %w", err)
	}
	if profile.Avatar == nil {
		return nil
	}

	candidate := blobCandidate{
		CID:      profile.Avatar.Ref.String(),
		MimeType: profile.Avatar.MimeType,
		Size:     profile.Avatar.Size,
	}
	blobRef, err := b.ensureBlob(ctx, atDID, candidate)
	if err != nil {
		return err
	}

	image := map[string]interface{}{
		"link": blobRef,
	}
	if candidate.MimeType != "" {
		image["type"] = candidate.MimeType
	}
	if candidate.Size > 0 {
		image["size"] = candidate.Size
	}
	mapped["image"] = image
	return nil
}

func postBlobCandidates(post *appbsky.FeedPost) []blobCandidate {
	if post == nil || post.Embed == nil {
		return nil
	}
	if post.Embed.EmbedImages != nil {
		return imageCandidates(post.Embed.EmbedImages.Images)
	}
	if post.Embed.EmbedVideo != nil {
		return []blobCandidate{videoCandidate(post.Embed.EmbedVideo, 0)}
	}
	if post.Embed.EmbedRecordWithMedia != nil && post.Embed.EmbedRecordWithMedia.Media != nil {
		media := post.Embed.EmbedRecordWithMedia.Media
		if media.EmbedImages != nil {
			return imageCandidates(media.EmbedImages.Images)
		}
		if media.EmbedVideo != nil {
			return []blobCandidate{videoCandidate(media.EmbedVideo, 0)}
		}
	}
	return nil
}

func imageCandidates(images []*appbsky.EmbedImages_Image) []blobCandidate {
	candidates := make([]blobCandidate, 0, len(images))
	for i, image := range images {
		if image == nil || image.Image == nil {
			continue
		}
		candidate := blobCandidate{
			CID:      image.Image.Ref.String(),
			MimeType: image.Image.MimeType,
			Size:     image.Image.Size,
			Label:    labelOrFallback(image.Alt, i+1),
		}
		if image.AspectRatio != nil {
			candidate.Width = int(image.AspectRatio.Width)
			candidate.Height = int(image.AspectRatio.Height)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func videoCandidate(video *appbsky.EmbedVideo, index int) blobCandidate {
	label := ""
	if video != nil && video.Alt != nil {
		label = *video.Alt
	}
	candidate := blobCandidate{
		Label: labelOrFallback(label, index+1),
	}
	if video == nil || video.Video == nil {
		return candidate
	}
	candidate.CID = video.Video.Ref.String()
	candidate.MimeType = video.Video.MimeType
	candidate.Size = video.Video.Size
	if video.AspectRatio != nil {
		candidate.Width = int(video.AspectRatio.Width)
		candidate.Height = int(video.AspectRatio.Height)
	}
	return candidate
}

func labelOrFallback(label string, index int) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Sprintf("bridged attachment %d", index)
	}
	return label
}

func appendMention(mapped map[string]interface{}, mention map[string]interface{}) {
	raw := mapped["mentions"]
	mentions := make([]map[string]interface{}, 0, 1)
	switch typed := raw.(type) {
	case []map[string]interface{}:
		mentions = append(mentions, typed...)
	case []interface{}:
		for _, item := range typed {
			if m, ok := item.(map[string]interface{}); ok {
				mentions = append(mentions, m)
			}
		}
	}

	link := asString(mention["link"])
	for _, existing := range mentions {
		if asString(existing["link"]) == link {
			return
		}
	}

	normalized := map[string]interface{}{
		"link": link,
	}
	if name := strings.TrimSpace(asString(mention["name"])); name != "" {
		normalized["name"] = name
	}
	if size, ok := mention["size"].(int64); ok && size > 0 {
		normalized["size"] = size
	}
	if width, ok := mention["width"].(int); ok && width > 0 {
		normalized["width"] = width
	}
	if height, ok := mention["height"].(int); ok && height > 0 {
		normalized["height"] = height
	}
	if mime := strings.TrimSpace(asString(mention["type"])); mime != "" {
		normalized["type"] = mime
	}

	mentions = append(mentions, normalized)
	mapped["mentions"] = mentions
}

func appendMarkdownBlock(text, block string) string {
	text = strings.TrimRight(text, "\n")
	block = strings.TrimSpace(block)
	if block == "" {
		return text
	}
	if text == "" {
		return block
	}
	return text + "\n\n" + block
}

func escapeMarkdownLabel(label string) string {
	label = strings.ReplaceAll(label, "]", "\\]")
	return label
}

func asString(raw interface{}) string {
	if raw == nil {
		return ""
	}
	return fmt.Sprint(raw)
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
		if b.resolver == nil {
			return "", fmt.Errorf("blob fetch unavailable for %s: no xrpc client or host resolver configured", cand.CID)
		}
	}

	client, err := b.clientForDID(ctx, atDID)
	if err != nil {
		return "", fmt.Errorf("resolve blob host did=%s: %w", atDID, err)
	}

	payload, err := atproto.SyncGetBlob(ctx, client, cand.CID, atDID)
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

func (b *Bridge) clientForDID(ctx context.Context, atDID string) (lexutil.LexClient, error) {
	if b.resolver != nil {
		host, err := b.resolver.ResolvePDSEndpoint(ctx, atDID)
		if err != nil {
			return nil, err
		}
		return &xrpc.Client{
			Host:   strings.TrimRight(strings.TrimSpace(host), "/"),
			Client: configuredHTTPClient(b.client),
		}, nil
	}
	if b.xrpc != nil {
		return b.xrpc, nil
	}
	return nil, fmt.Errorf("no blob fetch client configured")
}

func configuredHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}
