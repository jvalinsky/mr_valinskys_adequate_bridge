package bridge

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

const (
	reverseReceiveLogSeqKey = "reverse_receive_log_seq"
	reverseLastScanAtKey    = "reverse_sync_last_scan_at"
	reverseLastErrorKey     = "reverse_sync_last_error"
	reverseEnabledKey       = "reverse_sync_enabled"
	reverseCredentialsKey   = "reverse_sync_credentials_file"
	defaultReverseScanLimit = 100
	maxReverseImageEmbeds   = 4
)

var (
	reverseMarkdownBlobLinkPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)
	reverseBareURLPattern          = regexp.MustCompile(`https?://[^\s<>()]+`)
	reverseMultiSpacePattern       = regexp.MustCompile(`[ \t]{2,}`)
	reverseMultiBlankLinePattern   = regexp.MustCompile(`\n{3,}`)
)

type ReverseDatabase interface {
	GetReverseIdentityMapping(ctx context.Context, ssbFeedID string) (*db.ReverseIdentityMapping, error)
	AddReverseEvent(ctx context.Context, event db.ReverseEvent) error
	GetReverseEvent(ctx context.Context, sourceSSBMsgRef string) (*db.ReverseEvent, error)
	ResetReverseEventForRetry(ctx context.Context, sourceSSBMsgRef string) error
	GetLatestPublishedReverseFollow(ctx context.Context, atDID, targetATDID string) (*db.ReverseEvent, error)
	ResolveATDIDBySSBFeed(ctx context.Context, ssbFeedID string) (string, bool, error)
	GetMessageBySSBRef(ctx context.Context, ssbMsgRef string) (*db.Message, error)
	GetMessage(ctx context.Context, atURI string) (*db.Message, error)
	AddMessage(ctx context.Context, msg db.Message) error
	GetBlobBySSBRef(ctx context.Context, ssbBlobRef string) (*db.Blob, error)
	GetBridgeState(ctx context.Context, key string) (string, bool, error)
	SetBridgeState(ctx context.Context, key, value string) error
}

type ReversePDSHostResolver interface {
	ResolvePDSEndpoint(ctx context.Context, did string) (string, error)
}

type ReverseCredentialFileEntry struct {
	Identifier  string `json:"identifier"`
	PDSHost     string `json:"pds_host,omitempty"`
	PasswordEnv string `json:"password_env"`
}

type ReverseCredentialStatus struct {
	Configured bool
	Reason     string
	Identifier string
	PDSHost    string
}

type reverseResolvedCredential struct {
	DID        string
	Identifier string
	PDSHost    string
	Password   string
}

type ReverseCreatedRecord struct {
	URI           string
	CID           string
	Collection    string
	RawRecordJSON string
}

type ReverseBlobStore interface {
	Get(hash []byte) (io.ReadCloser, error)
}

type ReverseBlobFetcher interface {
	EnsureBlob(ctx context.Context, sourceFeedID string, ref *refs.BlobRef) error
}

type ReverseRecordSession interface {
	UploadBlob(ctx context.Context, input io.Reader, mimeType string) (*lexutil.LexBlob, error)
	CreateRecord(ctx context.Context, collection string, record any) (*ReverseCreatedRecord, error)
	DeleteRecord(ctx context.Context, atURI string) error
}

type ReverseRecordWriter interface {
	NewSession(ctx context.Context, cred reverseResolvedCredential) (ReverseRecordSession, error)
}

type ReverseSyncStatusProvider interface {
	Enabled() bool
	CredentialStatus(did string) ReverseCredentialStatus
	RetryEvent(ctx context.Context, sourceSSBMsgRef string) error
}

type ReverseProcessor struct {
	db           ReverseDatabase
	receiveLog   feedlog.Log
	blobStore    ReverseBlobStore
	blobFetcher  ReverseBlobFetcher
	writer       ReverseRecordWriter
	hostResolver ReversePDSHostResolver
	logger       *log.Logger
	credentials  map[string]ReverseCredentialFileEntry
	enabled      bool
}

type ReverseProcessorConfig struct {
	DB           ReverseDatabase
	ReceiveLog   feedlog.Log
	BlobStore    ReverseBlobStore
	BlobFetcher  ReverseBlobFetcher
	Writer       ReverseRecordWriter
	HostResolver ReversePDSHostResolver
	Logger       *log.Logger
	Credentials  map[string]ReverseCredentialFileEntry
	Enabled      bool
}

type ATProtoReverseWriter struct {
	HTTPClient *http.Client
	Insecure   bool
}

type reverseByteRange struct {
	start int
	end   int
}

func LoadReverseCredentials(path string) (map[string]ReverseCredentialFileEntry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return map[string]ReverseCredentialFileEntry{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read reverse credentials file %s: %w", path, err)
	}

	var creds map[string]ReverseCredentialFileEntry
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse reverse credentials file %s: %w", path, err)
	}
	if creds == nil {
		creds = map[string]ReverseCredentialFileEntry{}
	}

	normalized := make(map[string]ReverseCredentialFileEntry, len(creds))
	for did, entry := range creds {
		normalized[strings.TrimSpace(did)] = ReverseCredentialFileEntry{
			Identifier:  strings.TrimSpace(entry.Identifier),
			PDSHost:     strings.TrimRight(strings.TrimSpace(entry.PDSHost), "/"),
			PasswordEnv: strings.TrimSpace(entry.PasswordEnv),
		}
	}
	return normalized, nil
}

func NewReverseProcessor(cfg ReverseProcessorConfig) *ReverseProcessor {
	writer := cfg.Writer
	if writer == nil {
		writer = &ATProtoReverseWriter{}
	}
	creds := cfg.Credentials
	if creds == nil {
		creds = map[string]ReverseCredentialFileEntry{}
	}
	return &ReverseProcessor{
		db:           cfg.DB,
		receiveLog:   cfg.ReceiveLog,
		blobStore:    cfg.BlobStore,
		blobFetcher:  cfg.BlobFetcher,
		writer:       writer,
		hostResolver: cfg.HostResolver,
		logger:       logutil.Ensure(cfg.Logger),
		credentials:  creds,
		enabled:      cfg.Enabled,
	}
}

func (p *ReverseProcessor) Enabled() bool {
	return p != nil && p.enabled
}

func (p *ReverseProcessor) CredentialStatus(did string) ReverseCredentialStatus {
	status := ReverseCredentialStatus{}
	if p == nil {
		status.Reason = "reverse_sync_unavailable"
		return status
	}
	entry, ok := p.credentials[strings.TrimSpace(did)]
	if !ok {
		status.Reason = "missing_credentials_entry"
		return status
	}
	status.Identifier = entry.Identifier
	status.PDSHost = entry.PDSHost
	switch {
	case strings.TrimSpace(entry.Identifier) == "":
		status.Reason = "missing_identifier"
	case strings.TrimSpace(entry.PasswordEnv) == "":
		status.Reason = "missing_password_env"
	case strings.TrimSpace(os.Getenv(entry.PasswordEnv)) == "":
		status.Reason = "password_env_unset"
	default:
		status.Configured = true
		if strings.TrimSpace(entry.PDSHost) == "" {
			status.Reason = "configured_via_plc"
		} else {
			status.Reason = "configured"
		}
	}
	return status
}

func (p *ReverseProcessor) Run(ctx context.Context, interval time.Duration, batchSize int) {
	if p == nil || p.receiveLog == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if batchSize <= 0 {
		batchSize = defaultReverseScanLimit
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	p.runOnce(ctx, batchSize)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx, batchSize)
		}
	}
}

func (p *ReverseProcessor) runOnce(ctx context.Context, batchSize int) {
	if batchSize <= 0 {
		batchSize = defaultReverseScanLimit
	}
	processed, err := p.ProcessBatch(ctx, batchSize)
	_ = p.db.SetBridgeState(ctx, reverseLastScanAtKey, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		_ = p.db.SetBridgeState(ctx, reverseLastErrorKey, err.Error())
		p.logger.Printf("event=reverse_sync_batch_failed processed=%d err=%v", processed, err)
		return
	}
	_ = p.db.SetBridgeState(ctx, reverseLastErrorKey, "")
	if processed > 0 {
		p.logger.Printf("event=reverse_sync_batch processed=%d", processed)
	}
}

func (p *ReverseProcessor) ProcessBatch(ctx context.Context, batchSize int) (int, error) {
	if p == nil {
		return 0, fmt.Errorf("reverse processor is nil")
	}
	if p.receiveLog == nil {
		return 0, fmt.Errorf("reverse receive log is nil")
	}
	if p.db == nil {
		return 0, fmt.Errorf("reverse database is nil")
	}
	if batchSize <= 0 {
		batchSize = defaultReverseScanLimit
	}

	currentSeq, err := p.receiveLog.Seq()
	if err != nil {
		return 0, fmt.Errorf("reverse receive log seq: %w", err)
	}
	if currentSeq < 1 {
		return 0, nil
	}

	lastSeq, err := p.readLastProcessedSeq(ctx)
	if err != nil {
		return 0, err
	}

	processed := 0
	for seq := lastSeq + 1; seq <= currentSeq && processed < batchSize; seq++ {
		msg, err := p.receiveLog.Get(seq)
		if err != nil {
			return processed, fmt.Errorf("reverse receive log get seq %d: %w", seq, err)
		}
		if err := p.processStoredMessage(ctx, seq, msg, false); err != nil {
			return processed, err
		}
		if err := p.db.SetBridgeState(ctx, reverseReceiveLogSeqKey, fmt.Sprintf("%d", seq)); err != nil {
			return processed, fmt.Errorf("persist reverse receive log seq %d: %w", seq, err)
		}
		processed++
	}

	return processed, nil
}

func (p *ReverseProcessor) RetryEvent(ctx context.Context, sourceSSBMsgRef string) error {
	if p == nil {
		return fmt.Errorf("reverse processor is nil")
	}
	sourceSSBMsgRef = strings.TrimSpace(sourceSSBMsgRef)
	if sourceSSBMsgRef == "" {
		return fmt.Errorf("source SSB msg ref is required")
	}

	event, err := p.db.GetReverseEvent(ctx, sourceSSBMsgRef)
	if err != nil {
		return err
	}
	if event == nil {
		return fmt.Errorf("reverse event %s not found", sourceSSBMsgRef)
	}
	if event.EventState == db.ReverseEventStatePublished || event.EventState == db.ReverseEventStateSkipped {
		return fmt.Errorf("reverse event %s is not retryable", sourceSSBMsgRef)
	}

	if err := p.db.ResetReverseEventForRetry(ctx, sourceSSBMsgRef); err != nil {
		return err
	}
	return p.processDecodedMessage(ctx, event.ReceiveLogSeq, event.SourceSSBMsgRef, event.SourceSSBAuthor, event.SourceSSBSeq, []byte(event.RawSSBJSON), true)
}

func (p *ReverseProcessor) processStoredMessage(ctx context.Context, receiveLogSeq int64, msg *feedlog.StoredMessage, force bool) error {
	if msg == nil || msg.Metadata == nil {
		return nil
	}
	sourceRef := strings.TrimSpace(msg.Metadata.Hash)
	if sourceRef == "" {
		sourceRef = strings.TrimSpace(msg.Key)
	}
	return p.processDecodedMessage(ctx, receiveLogSeq, sourceRef, strings.TrimSpace(msg.Metadata.Author), int64Ptr(msg.Metadata.Sequence), msg.Value, force)
}

func (p *ReverseProcessor) processDecodedMessage(ctx context.Context, receiveLogSeq int64, sourceRef, sourceAuthor string, sourceSeq *int64, rawSSBJSON []byte, force bool) error {
	sourceRef = strings.TrimSpace(sourceRef)
	sourceAuthor = strings.TrimSpace(sourceAuthor)
	if sourceRef == "" || sourceAuthor == "" {
		return nil
	}
	if !force {
		existing, err := p.db.GetReverseEvent(ctx, sourceRef)
		if err != nil {
			return fmt.Errorf("load existing reverse event %s: %w", sourceRef, err)
		}
		if existing != nil && existing.EventState != db.ReverseEventStatePending {
			return nil
		}
	}

	mapping, err := p.db.GetReverseIdentityMapping(ctx, sourceAuthor)
	if err != nil {
		return fmt.Errorf("lookup reverse identity mapping %s: %w", sourceAuthor, err)
	}
	if mapping == nil || !mapping.Active {
		return nil
	}

	content, err := decodeSSBContent(rawSSBJSON)
	if err != nil {
		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, "", rawSSBJSON)
		event.EventState = db.ReverseEventStateFailed
		event.ErrorText = fmt.Sprintf("decode_ssb_json=%v", err)
		if addErr := p.persistReverseEvent(ctx, event); addErr != nil {
			return fmt.Errorf("persist reverse decode failure %s: %w", sourceRef, addErr)
		}
		return nil
	}

	event, record, deleteATURI, err := p.buildReverseEvent(ctx, receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, *mapping, content, rawSSBJSON)
	if err != nil {
		return err
	}
	if event == nil {
		return nil
	}

	if event.EventState == db.ReverseEventStateDeferred || event.EventState == db.ReverseEventStateSkipped {
		return p.persistReverseEvent(ctx, *event)
	}

	cred, credStatus := p.resolveCredential(ctx, event.ATDID)
	if !credStatus.Configured {
		event.EventState = db.ReverseEventStateDeferred
		event.DeferReason = "credentials_" + credStatus.Reason
		event.ErrorText = ""
		return p.persistReverseEvent(ctx, *event)
	}

	attemptedAt := time.Now().UTC()
	event.Attempts = 1
	event.LastAttemptAt = &attemptedAt

	session, err := p.writer.NewSession(ctx, cred)
	if err != nil {
		event.EventState = db.ReverseEventStateFailed
		event.ErrorText = err.Error()
		return p.persistReverseEvent(ctx, *event)
	}

	switch event.Action {
	case db.ReverseActionUnfollow:
		if err := session.DeleteRecord(ctx, deleteATURI); err != nil {
			event.EventState = db.ReverseEventStateFailed
			event.ErrorText = err.Error()
			return p.persistReverseEvent(ctx, *event)
		}
		event.EventState = db.ReverseEventStatePublished
		publishedAt := time.Now().UTC()
		event.PublishedAt = &publishedAt
		event.ResultATURI = deleteATURI
		event.RawATJSON = fmt.Sprintf(`{"op":"delete","at_uri":%q}`, deleteATURI)
	default:
		if post, ok := record.(*appbsky.FeedPost); ok && post != nil {
			deferReason, err := p.enrichReversePostRecord(ctx, session, event.SourceSSBAuthor, content, post)
			if err != nil {
				event.EventState = db.ReverseEventStateFailed
				event.ErrorText = err.Error()
				return p.persistReverseEvent(ctx, *event)
			}
			if deferReason != "" {
				event.EventState = db.ReverseEventStateDeferred
				event.DeferReason = deferReason
				event.ErrorText = ""
				return p.persistReverseEvent(ctx, *event)
			}
		} else if profile, ok := record.(*appbsky.ActorProfile); ok && profile != nil {
			deferReason, err := p.enrichReverseProfileRecord(ctx, session, event.SourceSSBAuthor, content, profile)
			if err != nil {
				event.EventState = db.ReverseEventStateFailed
				event.ErrorText = err.Error()
				return p.persistReverseEvent(ctx, *event)
			}
			if deferReason != "" {
				event.EventState = db.ReverseEventStateDeferred
				event.DeferReason = deferReason
				event.ErrorText = ""
				return p.persistReverseEvent(ctx, *event)
			}
		}

		created, err := session.CreateRecord(ctx, event.ResultCollection, record)
		if err != nil {
			event.EventState = db.ReverseEventStateFailed
			event.ErrorText = err.Error()
			return p.persistReverseEvent(ctx, *event)
		}
		event.EventState = db.ReverseEventStatePublished
		publishedAt := time.Now().UTC()
		event.PublishedAt = &publishedAt
		event.ResultATURI = created.URI
		event.ResultATCID = created.CID
		event.RawATJSON = created.RawRecordJSON

		if err := p.persistForwardCorrelation(ctx, *event, record); err != nil {
			return fmt.Errorf("persist forward correlation for %s: %w", sourceRef, err)
		}
	}

	return p.persistReverseEvent(ctx, *event)
}

func (p *ReverseProcessor) enrichReversePostRecord(ctx context.Context, session ReverseRecordSession, sourceFeedID string, content map[string]any, post *appbsky.FeedPost) (string, error) {
	if post == nil {
		return "", nil
	}

	mentions := normalizedReverseMentions(content["mentions"])
	images, embeddedRefs, deferReason, err := p.buildReverseImageEmbeds(ctx, session, sourceFeedID, mentions)
	if err != nil || deferReason != "" {
		return deferReason, err
	}

	text := stripEmbeddedBlobMarkdown(post.Text, embeddedRefs)
	post.Text = text
	post.Facets, err = p.buildReverseFacets(ctx, text, mentions, embeddedRefs)
	if err != nil {
		return "", err
	}
	if len(images) > 0 {
		post.Embed = &appbsky.FeedPost_Embed{
			EmbedImages: &appbsky.EmbedImages{Images: images},
		}
	}
	return "", nil
}

func (p *ReverseProcessor) enrichReverseProfileRecord(ctx context.Context, session ReverseRecordSession, sourceFeedID string, content map[string]any, profile *appbsky.ActorProfile) (string, error) {
	if profile == nil {
		return "", nil
	}
	link := strings.TrimSpace(stringValue(content["image"]))
	if link == "" || !strings.HasPrefix(link, "&") {
		return "", nil
	}
	if p.blobStore == nil {
		return "blob_store_unavailable", nil
	}
	blobMeta, err := p.db.GetBlobBySSBRef(ctx, link)
	if err != nil {
		return "", fmt.Errorf("lookup blob metadata %s: %w", link, err)
	}
	mimeType := ""
	if blobMeta != nil {
		mimeType = strings.TrimSpace(blobMeta.MimeType)
	}
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return "blob_media_unsupported=" + link, nil
	}
	blobRef, err := refs.ParseBlobRef(link)
	if err != nil {
		return "blob_ref_invalid=" + link, nil
	}
	reader, err := p.blobStore.Get(blobRef.Hash())
	if err != nil {
		if p.blobFetcher == nil || p.blobFetcher.EnsureBlob(ctx, sourceFeedID, blobRef) != nil {
			return "blob_read_failed=" + link, nil
		}
		reader, err = p.blobStore.Get(blobRef.Hash())
		if err != nil {
			return "blob_read_failed=" + link, nil
		}
	}
	uploaded, uploadErr := session.UploadBlob(ctx, reader, mimeType)
	closeErr := reader.Close()
	if uploadErr != nil {
		return "blob_upload_failed=" + link, nil
	}
	if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		return "", fmt.Errorf("close blob reader %s: %w", link, closeErr)
	}
	if uploaded == nil {
		return "blob_upload_failed=" + link, nil
	}
	
	profile.Avatar = &appbsky.LexBlob{
		Ref:      uploaded.Ref,
		MimeType: uploaded.MimeType,
		Size:     uploaded.Size,
	}
	return "", nil
}

func (p *ReverseProcessor) buildReverseImageEmbeds(ctx context.Context, session ReverseRecordSession, sourceFeedID string, mentions []map[string]any) ([]*appbsky.EmbedImages_Image, map[string]struct{}, string, error) {
	embeddedRefs := make(map[string]struct{})
	if len(mentions) == 0 {
		return nil, embeddedRefs, "", nil
	}
	if p.blobStore == nil {
		for _, mention := range mentions {
			if strings.HasPrefix(strings.TrimSpace(stringValue(mention["link"])), "&") {
				return nil, nil, "blob_store_unavailable", nil
			}
		}
		return nil, embeddedRefs, "", nil
	}

	images := make([]*appbsky.EmbedImages_Image, 0, maxReverseImageEmbeds)
	seenRefs := make(map[string]string)
	for _, mention := range mentions {
		link := strings.TrimSpace(stringValue(mention["link"]))
		if !strings.HasPrefix(link, "&") {
			continue
		}
		if existingMime, seen := seenRefs[link]; seen {
			currentMime := strings.TrimSpace(stringValue(mention["type"]))
			if currentMime != "" && existingMime != "" && !strings.EqualFold(currentMime, existingMime) {
				return nil, nil, "blob_mime_mismatch=" + link, nil
			}
			embeddedRefs[link] = struct{}{}
			continue
		}

		blobMeta, err := p.db.GetBlobBySSBRef(ctx, link)
		if err != nil {
			return nil, nil, "", fmt.Errorf("lookup blob metadata %s: %w", link, err)
		}

		mentionMime := strings.TrimSpace(stringValue(mention["type"]))
		metaMime := ""
		if blobMeta != nil {
			metaMime = strings.TrimSpace(blobMeta.MimeType)
		}
		if mentionMime != "" && metaMime != "" && !strings.EqualFold(mentionMime, metaMime) {
			return nil, nil, "blob_mime_mismatch=" + link, nil
		}
		mimeType := mentionMime
		if mimeType == "" {
			mimeType = metaMime
		}
		if mimeType == "" {
			return nil, nil, "blob_mime_missing=" + link, nil
		}
		if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			return nil, nil, "blob_media_unsupported=" + link, nil
		}
		if len(images) >= maxReverseImageEmbeds {
			return nil, nil, fmt.Sprintf("image_limit_exceeded=%d", len(images)+1), nil
		}

		blobRef, err := refs.ParseBlobRef(link)
		if err != nil {
			return nil, nil, "blob_ref_invalid=" + link, nil
		}
		reader, err := p.blobStore.Get(blobRef.Hash())
		if err != nil {
			if p.blobFetcher == nil || p.blobFetcher.EnsureBlob(ctx, sourceFeedID, blobRef) != nil {
				return nil, nil, "blob_read_failed=" + link, nil
			}
			reader, err = p.blobStore.Get(blobRef.Hash())
			if err != nil {
				return nil, nil, "blob_read_failed=" + link, nil
			}
		}
		uploaded, uploadErr := session.UploadBlob(ctx, reader, mimeType)
		closeErr := reader.Close()
		if uploadErr != nil {
			return nil, nil, "blob_upload_failed=" + link, nil
		}
		if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			return nil, nil, "", fmt.Errorf("close blob reader %s: %w", link, closeErr)
		}
		if uploaded == nil {
			return nil, nil, "blob_upload_failed=" + link, nil
		}
		uploadMime := strings.TrimSpace(uploaded.MimeType)
		if uploadMime == "" {
			uploaded.MimeType = mimeType
			uploadMime = mimeType
		}
		if !strings.HasPrefix(strings.ToLower(uploadMime), "image/") {
			return nil, nil, "blob_media_unsupported=" + link, nil
		}
		if mimeType != "" && uploadMime != "" && !strings.EqualFold(mimeType, uploadMime) {
			return nil, nil, "blob_mime_mismatch=" + link, nil
		}

		images = append(images, &appbsky.EmbedImages_Image{
			Alt:   strings.TrimSpace(stringValue(mention["name"])),
			Image: &appbsky.LexBlob{Ref: uploaded.Ref, MimeType: uploaded.MimeType, Size: uploaded.Size},
		})
		seenRefs[link] = mimeType
		embeddedRefs[link] = struct{}{}
	}

	return images, embeddedRefs, "", nil
}

func (p *ReverseProcessor) buildReverseFacets(ctx context.Context, text string, mentions []map[string]any, embeddedRefs map[string]struct{}) ([]*appbsky.RichtextFacet, error) {
	if strings.TrimSpace(text) == "" && len(mentions) == 0 {
		return nil, nil
	}

	occupied := make([]reverseByteRange, 0, len(mentions))
	facets := make([]*appbsky.RichtextFacet, 0, len(mentions))

	appendFacet := func(start, end int, feature *appbsky.RichtextFacet_Features_Elem) {
		if start < 0 || end <= start || feature == nil {
			return
		}
		occupied = append(occupied, reverseByteRange{start: start, end: end})
		facets = append(facets, &appbsky.RichtextFacet{
			Index: &appbsky.RichtextFacet_ByteSlice{
				ByteStart: int64(start),
				ByteEnd:   int64(end),
			},
			Features: []*appbsky.RichtextFacet_Features_Elem{feature},
		})
	}

	for _, mention := range mentions {
		link := strings.TrimSpace(stringValue(mention["link"]))
		if link == "" {
			continue
		}
		if _, isEmbeddedBlob := embeddedRefs[link]; isEmbeddedBlob {
			continue
		}

		switch {
		case strings.HasPrefix(link, "&"):
			return nil, fmt.Errorf("unsupported blob mention remained after embed resolution: %s", link)
		case strings.HasPrefix(link, "@"):
			atDID, ok, err := p.db.ResolveATDIDBySSBFeed(ctx, link)
			if err != nil {
				return nil, fmt.Errorf("resolve reverse mention did %s: %w", link, err)
			}
			if !ok || strings.TrimSpace(atDID) == "" {
				continue
			}
			token := firstNonBlank(strings.TrimSpace(stringValue(mention["name"])), link)
			start, end, ok := findFirstNonOverlappingToken(text, token, occupied)
			if !ok {
				continue
			}
			appendFacet(start, end, &appbsky.RichtextFacet_Features_Elem{
				RichtextFacet_Mention: &appbsky.RichtextFacet_Mention{Did: atDID},
			})
		case strings.HasPrefix(link, "#"):
			tag := strings.TrimSpace(strings.TrimPrefix(link, "#"))
			if tag == "" {
				continue
			}
			token := firstNonBlank(strings.TrimSpace(stringValue(mention["name"])), "#"+tag)
			start, end, ok := findFirstNonOverlappingToken(text, token, occupied)
			if !ok {
				continue
			}
			appendFacet(start, end, &appbsky.RichtextFacet_Features_Elem{
				RichtextFacet_Tag: &appbsky.RichtextFacet_Tag{Tag: tag},
			})
		case isHTTPURL(link):
			token := firstNonBlank(strings.TrimSpace(stringValue(mention["name"])), link)
			start, end, ok := findFirstNonOverlappingToken(text, token, occupied)
			if !ok {
				continue
			}
			appendFacet(start, end, &appbsky.RichtextFacet_Features_Elem{
				RichtextFacet_Link: &appbsky.RichtextFacet_Link{Uri: link},
			})
		}
	}

	for _, match := range reverseBareURLPattern.FindAllStringIndex(text, -1) {
		start := match[0]
		end := match[1]
		rawURL := text[start:end]
		trimmedURL := strings.TrimRight(rawURL, ".,!?;:")
		if trimmedURL == "" {
			continue
		}
		end = start + len(trimmedURL)
		if rangeOverlaps(start, end, occupied) {
			continue
		}
		appendFacet(start, end, &appbsky.RichtextFacet_Features_Elem{
			RichtextFacet_Link: &appbsky.RichtextFacet_Link{Uri: trimmedURL},
		})
	}

	if len(facets) == 0 {
		return nil, nil
	}
	return facets, nil
}

func stripEmbeddedBlobMarkdown(text string, embeddedRefs map[string]struct{}) string {
	if len(embeddedRefs) == 0 || text == "" {
		return text
	}

	matches := reverseMarkdownBlobLinkPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		wholeStart, wholeEnd := match[0], match[1]
		target := strings.TrimSpace(text[match[2]:match[3]])
		if _, ok := embeddedRefs[target]; !ok {
			continue
		}
		builder.WriteString(text[last:wholeStart])
		last = wholeEnd
	}
	builder.WriteString(text[last:])
	return normalizeReversePostText(builder.String())
}

func normalizeReversePostText(text string) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = reverseMultiSpacePattern.ReplaceAllString(line, " ")
		lines[i] = strings.TrimSpace(line)
	}
	text = strings.Join(lines, "\n")
	text = reverseMultiBlankLinePattern.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func normalizedReverseMentions(raw any) []map[string]any {
	switch typed := raw.(type) {
	case nil:
		return nil
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]any)
			if ok {
				out = append(out, m)
				continue
			}
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func findFirstNonOverlappingToken(text, token string, occupied []reverseByteRange) (int, int, bool) {
	token = strings.TrimSpace(token)
	if token == "" || text == "" {
		return 0, 0, false
	}
	searchFrom := 0
	for searchFrom < len(text) {
		idx := strings.Index(text[searchFrom:], token)
		if idx < 0 {
			return 0, 0, false
		}
		start := searchFrom + idx
		end := start + len(token)
		if !rangeOverlaps(start, end, occupied) {
			return start, end, true
		}
		searchFrom = start + 1
	}
	return 0, 0, false
}

func rangeOverlaps(start, end int, occupied []reverseByteRange) bool {
	if end <= start {
		return true
	}
	for _, used := range occupied {
		if start < used.end && end > used.start {
			return true
		}
	}
	return false
}

func decodeSSBContent(rawSSBJSON []byte) (map[string]any, error) {
	rawSSBJSON = bytes.TrimSpace(rawSSBJSON)
	if len(rawSSBJSON) == 0 {
		return nil, fmt.Errorf("empty_ssb_json")
	}

	if signed, _, err := legacy.ParseSignedMessageJSON(rawSSBJSON); err == nil {
		return normalizeSSBContentMap(signed.Content)
	}

	var envelope map[string]any
	if err := json.Unmarshal(rawSSBJSON, &envelope); err != nil {
		return nil, err
	}
	if content, ok := envelope["content"]; ok {
		return normalizeSSBContentMap(content)
	}
	return normalizeSSBContentMap(envelope)
}

func normalizeSSBContentMap(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, nil
	case json.RawMessage:
		var out map[string]any
		if err := json.Unmarshal(typed, &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func (p *ReverseProcessor) buildReverseEvent(ctx context.Context, receiveLogSeq int64, sourceRef, sourceAuthor string, sourceSeq *int64, mapping db.ReverseIdentityMapping, content map[string]any, rawSSBJSON []byte) (*db.ReverseEvent, any, string, error) {
	msgType := strings.TrimSpace(stringValue(content["type"]))
	switch msgType {
	case "post":
		if replyRootRef, replyParentRef := extractReplyRefs(content); replyRootRef != "" || replyParentRef != "" {
			event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionReply, rawSSBJSON)
			event.ResultCollection = mapper.RecordTypePost
			if !mapping.AllowReplies {
				event.EventState = db.ReverseEventStateSkipped
				event.DeferReason = "action_disabled=reply"
				return &event, nil, "", nil
			}
			rootMsg, parentMsg, deferReason, err := p.resolveReplyTargets(ctx, replyRootRef, replyParentRef)
			if err != nil {
				return nil, nil, "", err
			}
			event.TargetSSBRef = replyParentRef
			if deferReason != "" {
				event.EventState = db.ReverseEventStateDeferred
				event.DeferReason = deferReason
				return &event, nil, "", nil
			}
			event.TargetATURI = parentMsg.ATURI
			event.TargetATCID = parentMsg.ATCID
			if rootMsg != nil {
				event.TargetATDID = rootMsg.ATDID
			}
			record := &appbsky.FeedPost{
				Text:      stringValue(content["text"]),
				CreatedAt: reverseCreatedAt(sourceSeq),
				Reply: &appbsky.FeedPost_ReplyRef{
					Root:   &appbsky.RepoStrongRef{Uri: rootMsg.ATURI, Cid: rootMsg.ATCID},
					Parent: &appbsky.RepoStrongRef{Uri: parentMsg.ATURI, Cid: parentMsg.ATCID},
				},
			}
			return &event, record, "", nil
		}

		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionPost, rawSSBJSON)
		event.ResultCollection = mapper.RecordTypePost
		if !mapping.AllowPosts {
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "action_disabled=post"
			return &event, nil, "", nil
		}
		record := &appbsky.FeedPost{
			Text:      stringValue(content["text"]),
			CreatedAt: reverseCreatedAt(sourceSeq),
		}
		return &event, record, "", nil
	case "repost", "share":
		targetRef := strings.TrimSpace(stringValue(content["repost"]))
		if targetRef == "" {
			targetRef = strings.TrimSpace(stringValue(content["share"]))
		}
		if targetRef == "" {
			event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionRepost, rawSSBJSON)
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "missing_repost_target"
			return &event, nil, "", nil
		}
		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionRepost, rawSSBJSON)
		event.ResultCollection = mapper.RecordTypeRepost
		if !mapping.AllowPosts {
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "action_disabled=post"
			return &event, nil, "", nil
		}

		targetMsg, err := p.db.GetMessageBySSBRef(ctx, targetRef)
		if err != nil {
			return nil, nil, "", fmt.Errorf("lookup repost target %s: %w", targetRef, err)
		}
		if targetMsg == nil || strings.TrimSpace(targetMsg.ATURI) == "" || strings.TrimSpace(targetMsg.ATCID) == "" {
			event.EventState = db.ReverseEventStateDeferred
			event.DeferReason = "repost_target_unmapped=" + targetRef
			return &event, nil, "", nil
		}
		event.TargetSSBRef = targetRef
		event.TargetATURI = targetMsg.ATURI
		event.TargetATCID = targetMsg.ATCID
		event.TargetATDID = targetMsg.ATDID

		record := &appbsky.FeedRepost{
			Subject: &appbsky.RepoStrongRef{
				Uri: targetMsg.ATURI,
				Cid: targetMsg.ATCID,
			},
			CreatedAt: reverseCreatedAt(sourceSeq),
		}
		return &event, record, "", nil

	case "vote":
		voteVal, ok := content["vote"].(map[string]any)
		if !ok {
			return nil, nil, "", nil
		}
		value := floatValue(voteVal["value"])
		if value <= 0 {
			return nil, nil, "", nil
		}
		targetRef := strings.TrimSpace(stringValue(voteVal["link"]))
		if targetRef == "" {
			event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionVote, rawSSBJSON)
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "missing_vote_target"
			return &event, nil, "", nil
		}
		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionVote, rawSSBJSON)
		event.ResultCollection = mapper.RecordTypeLike
		if !mapping.AllowPosts {
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "action_disabled=like"
			return &event, nil, "", nil
		}

		targetMsg, err := p.db.GetMessageBySSBRef(ctx, targetRef)
		if err != nil {
			return nil, nil, "", fmt.Errorf("lookup vote target %s: %w", targetRef, err)
		}
		if targetMsg == nil || strings.TrimSpace(targetMsg.ATURI) == "" || strings.TrimSpace(targetMsg.ATCID) == "" {
			event.EventState = db.ReverseEventStateDeferred
			event.DeferReason = "vote_target_unmapped=" + targetRef
			return &event, nil, "", nil
		}
		event.TargetSSBRef = targetRef
		event.TargetATURI = targetMsg.ATURI
		event.TargetATCID = targetMsg.ATCID
		event.TargetATDID = targetMsg.ATDID

		record := &appbsky.FeedLike{
			Subject: &appbsky.RepoStrongRef{
				Uri: targetMsg.ATURI,
				Cid: targetMsg.ATCID,
			},
			CreatedAt: reverseCreatedAt(sourceSeq),
		}
		return &event, record, "", nil

	case "about":
		targetFeed := strings.TrimSpace(stringValue(content["about"]))
		if targetFeed == "" || targetFeed != sourceAuthor {
			event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionAbout, rawSSBJSON)
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "about_not_self"
			return &event, nil, "", nil
		}
		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionAbout, rawSSBJSON)
		event.ResultCollection = mapper.RecordTypeProfile
		
		name := stringValue(content["name"])
		description := stringValue(content["description"])
		
		record := &appbsky.ActorProfile{}
		if name != "" {
			record.DisplayName = &name
		}
		if description != "" {
			record.Description = &description
		}
		
		return &event, record, "", nil

	case "contact":
		targetFeed := strings.TrimSpace(stringValue(content["contact"]))
		if targetFeed == "" {
			event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionFollow, rawSSBJSON)
			event.ResultCollection = mapper.RecordTypeFollow
			event.EventState = db.ReverseEventStateDeferred
			event.DeferReason = "missing_contact_feed"
			return &event, nil, "", nil
		}
		blocking := boolValue(content["blocking"])
		following := boolValue(content["following"])
		if blocking {
			event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, db.ReverseActionFollow, rawSSBJSON)
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "unsupported_contact_block"
			event.TargetSSBFeedID = targetFeed
			return &event, nil, "", nil
		}

		targetATDID, ok, err := p.db.ResolveATDIDBySSBFeed(ctx, targetFeed)
		if err != nil {
			return nil, nil, "", fmt.Errorf("resolve at did by ssb feed %s: %w", targetFeed, err)
		}
		action := db.ReverseActionUnfollow
		if following {
			action = db.ReverseActionFollow
		}
		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, action, rawSSBJSON)
		event.TargetSSBFeedID = targetFeed
		event.TargetATDID = targetATDID
		event.ResultCollection = mapper.RecordTypeFollow
		if !mapping.AllowFollows {
			event.EventState = db.ReverseEventStateSkipped
			event.DeferReason = "action_disabled=follow"
			return &event, nil, "", nil
		}
		if !ok || strings.TrimSpace(targetATDID) == "" {
			event.EventState = db.ReverseEventStateDeferred
			event.DeferReason = "target_did_unmapped=" + targetFeed
			return &event, nil, "", nil
		}

		if following {
			record := &appbsky.GraphFollow{
				Subject:   targetATDID,
				CreatedAt: reverseCreatedAt(sourceSeq),
			}
			return &event, record, "", nil
		}

		followEvent, err := p.db.GetLatestPublishedReverseFollow(ctx, mapping.ATDID, targetATDID)
		if err != nil {
			return nil, nil, "", err
		}
		if followEvent == nil || strings.TrimSpace(followEvent.ResultATURI) == "" {
			event.EventState = db.ReverseEventStateDeferred
			event.DeferReason = "follow_record_not_found=" + targetATDID
			return &event, nil, "", nil
		}
		event.TargetATURI = followEvent.ResultATURI
		event.TargetATCID = followEvent.ResultATCID
		return &event, nil, followEvent.ResultATURI, nil
	}

	return nil, nil, "", nil
}

func (p *ReverseProcessor) resolveReplyTargets(ctx context.Context, rootRef, parentRef string) (*db.Message, *db.Message, string, error) {
	rootRef = strings.TrimSpace(rootRef)
	parentRef = strings.TrimSpace(parentRef)
	if rootRef == "" && parentRef != "" {
		rootRef = parentRef
	}
	if parentRef == "" && rootRef != "" {
		parentRef = rootRef
	}
	if rootRef == "" || parentRef == "" {
		return nil, nil, "missing_reply_refs", nil
	}

	rootMsg, err := p.db.GetMessageBySSBRef(ctx, rootRef)
	if err != nil {
		return nil, nil, "", fmt.Errorf("lookup reply root %s: %w", rootRef, err)
	}
	if rootMsg == nil || strings.TrimSpace(rootMsg.ATURI) == "" || strings.TrimSpace(rootMsg.ATCID) == "" {
		return nil, nil, "reply_root_unmapped=" + rootRef, nil
	}

	parentMsg, err := p.db.GetMessageBySSBRef(ctx, parentRef)
	if err != nil {
		return nil, nil, "", fmt.Errorf("lookup reply parent %s: %w", parentRef, err)
	}
	if parentMsg == nil || strings.TrimSpace(parentMsg.ATURI) == "" || strings.TrimSpace(parentMsg.ATCID) == "" {
		return nil, nil, "reply_parent_unmapped=" + parentRef, nil
	}

	return rootMsg, parentMsg, "", nil
}

func (p *ReverseProcessor) persistForwardCorrelation(ctx context.Context, event db.ReverseEvent, record any) error {
	if strings.TrimSpace(event.ResultATURI) == "" {
		return nil
	}
	msg := db.Message{
		ATURI:        event.ResultATURI,
		ATCID:        event.ResultATCID,
		SSBMsgRef:    event.SourceSSBMsgRef,
		ATDID:        event.ATDID,
		Type:         event.ResultCollection,
		MessageState: db.MessageStatePublished,
		RawATJson:    event.RawATJSON,
		RawSSBJson:   event.RawSSBJSON,
	}
	if event.PublishedAt != nil {
		msg.PublishedAt = event.PublishedAt
	}
	if post, ok := record.(*appbsky.FeedPost); ok && post != nil && post.Reply != nil {
		if post.Reply.Root != nil {
			msg.RootATURI = strings.TrimSpace(post.Reply.Root.Uri)
		}
		if post.Reply.Parent != nil {
			msg.ParentATURI = strings.TrimSpace(post.Reply.Parent.Uri)
		}
	}
	return p.db.AddMessage(ctx, msg)
}

func (p *ReverseProcessor) resolveCredential(ctx context.Context, did string) (reverseResolvedCredential, ReverseCredentialStatus) {
	status := p.CredentialStatus(did)
	if !status.Configured {
		return reverseResolvedCredential{}, status
	}
	entry := p.credentials[strings.TrimSpace(did)]
	host := strings.TrimSpace(entry.PDSHost)
	if host == "" && p.hostResolver != nil {
		resolved, err := p.hostResolver.ResolvePDSEndpoint(ctx, did)
		if err != nil {
			status.Configured = false
			status.Reason = "pds_host_unresolved"
			return reverseResolvedCredential{}, status
		}
		host = resolved
	}
	if host == "" {
		status.Configured = false
		status.Reason = "missing_pds_host"
		return reverseResolvedCredential{}, status
	}
	return reverseResolvedCredential{
		DID:        strings.TrimSpace(did),
		Identifier: entry.Identifier,
		PDSHost:    strings.TrimRight(host, "/"),
		Password:   strings.TrimSpace(os.Getenv(entry.PasswordEnv)),
	}, status
}

func (p *ReverseProcessor) readLastProcessedSeq(ctx context.Context) (int64, error) {
	value, ok, err := p.db.GetBridgeState(ctx, reverseReceiveLogSeqKey)
	if err != nil {
		return 0, fmt.Errorf("get reverse receive log seq: %w", err)
	}
	if !ok || strings.TrimSpace(value) == "" {
		return 0, nil
	}
	var seq int64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &seq); err != nil {
		return 0, fmt.Errorf("parse reverse receive log seq %q: %w", value, err)
	}
	return seq, nil
}

func (p *ReverseProcessor) baseReverseEvent(receiveLogSeq int64, sourceRef, sourceAuthor string, sourceSeq *int64, atDID, action string, rawSSBJSON []byte) db.ReverseEvent {
	return db.ReverseEvent{
		SourceSSBMsgRef: sourceRef,
		SourceSSBAuthor: sourceAuthor,
		SourceSSBSeq:    sourceSeq,
		ReceiveLogSeq:   receiveLogSeq,
		ATDID:           atDID,
		Action:          action,
		EventState:      db.ReverseEventStatePending,
		RawSSBJSON:      string(rawSSBJSON),
	}
}

func (p *ReverseProcessor) persistReverseEvent(ctx context.Context, event db.ReverseEvent) error {
	if p != nil && p.logger != nil {
		p.logger.Printf(
			"event=reverse_sync_event source_ref=%s source_author=%s action=%s state=%s defer_reason=%s result_at_uri=%s err=%s",
			strings.TrimSpace(event.SourceSSBMsgRef),
			strings.TrimSpace(event.SourceSSBAuthor),
			strings.TrimSpace(event.Action),
			strings.TrimSpace(event.EventState),
			strings.TrimSpace(event.DeferReason),
			strings.TrimSpace(event.ResultATURI),
			strings.TrimSpace(event.ErrorText),
		)
	}
	return p.db.AddReverseEvent(ctx, event)
}

type atprotoReverseSession struct {
	client *xrpc.Client
	did    string
}

func (w *ATProtoReverseWriter) NewSession(ctx context.Context, cred reverseResolvedCredential) (ReverseRecordSession, error) {
	client, err := w.createSession(ctx, cred)
	if err != nil {
		return nil, err
	}
	return &atprotoReverseSession{client: client, did: cred.DID}, nil
}

func (s *atprotoReverseSession) UploadBlob(ctx context.Context, input io.Reader, mimeType string) (*lexutil.LexBlob, error) {
	resp, err := atproto.RepoUploadBlobWithMime(ctx, s.client, mimeType, input)
	if err != nil {
		return nil, fmt.Errorf("upload blob for %s: %w", s.did, err)
	}
	if resp == nil || resp.Blob == nil {
		return nil, fmt.Errorf("upload blob for %s: empty response", s.did)
	}
	return resp.Blob, nil
}

func (s *atprotoReverseSession) CreateRecord(ctx context.Context, collection string, record any) (*ReverseCreatedRecord, error) {
	resp, err := atproto.RepoCreateRecord(ctx, s.client, &atproto.RepoCreateRecord_Input{
		Collection: collection,
		Repo:       s.client.Auth.Did,
		Record:     &lexutil.LexiconTypeDecoder{Val: record},
	})
	if err != nil {
		return nil, fmt.Errorf("create record %s for %s: %w", collection, s.did, err)
	}
	rawRecordJSON, _ := json.Marshal(record)
	return &ReverseCreatedRecord{
		URI:           resp.Uri,
		CID:           resp.Cid,
		Collection:    collection,
		RawRecordJSON: string(rawRecordJSON),
	}, nil
}

func (s *atprotoReverseSession) DeleteRecord(ctx context.Context, atURI string) error {
	parsed, err := syntax.ParseATURI(atURI)
	if err != nil {
		return fmt.Errorf("parse at uri %s: %w", atURI, err)
	}
	if parsed.Collection().String() == "" || parsed.RecordKey().String() == "" {
		return fmt.Errorf("at uri %s is missing collection or rkey", atURI)
	}
	_, err = atproto.RepoDeleteRecord(ctx, s.client, &atproto.RepoDeleteRecord_Input{
		Collection: parsed.Collection().String(),
		Repo:       s.client.Auth.Did,
		Rkey:       parsed.RecordKey().String(),
	})
	if err != nil {
		return fmt.Errorf("delete record %s for %s: %w", atURI, s.did, err)
	}
	return nil
}

func (w *ATProtoReverseWriter) createSession(ctx context.Context, cred reverseResolvedCredential) (*xrpc.Client, error) {
	httpClient := w.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if w.Insecure {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: 30 * time.Second,
		}
	}
	client := &xrpc.Client{
		Host:   cred.PDSHost,
		Client: httpClient,
	}
	sess, err := atproto.ServerCreateSession(ctx, client, &atproto.ServerCreateSession_Input{
		Identifier: cred.Identifier,
		Password:   cred.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("create session for %s: %w", cred.Identifier, err)
	}
	client.Auth = &xrpc.AuthInfo{
		AccessJwt:  sess.AccessJwt,
		RefreshJwt: sess.RefreshJwt,
		Handle:     sess.Handle,
		Did:        sess.Did,
	}
	return client, nil
}

func NewDefaultReverseHostResolver(plcURL string, httpClient *http.Client) ReversePDSHostResolver {
	return backfill.DIDPDSResolver{
		PLCURL:     plcURL,
		HTTPClient: httpClient,
	}
}

func extractReplyRefs(content map[string]any) (string, string) {
	root := strings.TrimSpace(stringValue(content["root"]))
	parent := strings.TrimSpace(stringValue(content["branch"]))

	if tangles, ok := content["tangles"].(map[string]any); ok {
		if comment, ok := tangles["comment"].(map[string]any); ok {
			if root == "" {
				root = strings.TrimSpace(stringValue(comment["root"]))
			}
			if parent == "" {
				switch previous := comment["previous"].(type) {
				case []any:
					for i := len(previous) - 1; i >= 0; i-- {
						if ref := strings.TrimSpace(stringValue(previous[i])); ref != "" {
							parent = ref
							break
						}
					}
				case []string:
					if len(previous) > 0 {
						parent = strings.TrimSpace(previous[len(previous)-1])
					}
				}
			}
		}
	}

	if parent == "" {
		parent = root
	}
	if root == "" {
		root = parent
	}
	return root, parent
}

func reverseCreatedAt(sourceSeq *int64) string {
	_ = sourceSeq
	return time.Now().UTC().Format(time.RFC3339)
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func boolValue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

func floatValue(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isHTTPURL(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func int64Ptr(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}
