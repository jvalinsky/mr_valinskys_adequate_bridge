package bridge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

const (
	reverseReceiveLogSeqKey  = "reverse_receive_log_seq"
	reverseLastScanAtKey     = "reverse_sync_last_scan_at"
	reverseLastErrorKey      = "reverse_sync_last_error"
	reverseEnabledKey        = "reverse_sync_enabled"
	reverseCredentialsKey    = "reverse_sync_credentials_file"
	defaultReverseScanLimit  = 100
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
	URI            string
	CID            string
	Collection     string
	RawRecordJSON  string
}

type ReverseRecordWriter interface {
	CreateRecord(ctx context.Context, cred reverseResolvedCredential, collection string, record any) (*ReverseCreatedRecord, error)
	DeleteRecord(ctx context.Context, cred reverseResolvedCredential, atURI string) error
}

type ReverseSyncStatusProvider interface {
	Enabled() bool
	CredentialStatus(did string) ReverseCredentialStatus
	RetryEvent(ctx context.Context, sourceSSBMsgRef string) error
}

type ReverseProcessor struct {
	db          ReverseDatabase
	receiveLog  feedlog.Log
	writer      ReverseRecordWriter
	hostResolver ReversePDSHostResolver
	logger      *log.Logger
	credentials map[string]ReverseCredentialFileEntry
	enabled     bool
}

type ReverseProcessorConfig struct {
	DB           ReverseDatabase
	ReceiveLog   feedlog.Log
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

	var content map[string]any
	if err := json.Unmarshal(rawSSBJSON, &content); err != nil {
		event := p.baseReverseEvent(receiveLogSeq, sourceRef, sourceAuthor, sourceSeq, mapping.ATDID, "", rawSSBJSON)
		event.EventState = db.ReverseEventStateFailed
		event.ErrorText = fmt.Sprintf("decode_ssb_json=%v", err)
		if addErr := p.db.AddReverseEvent(ctx, event); addErr != nil {
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
		return p.db.AddReverseEvent(ctx, *event)
	}

	cred, credStatus := p.resolveCredential(ctx, event.ATDID)
	if !credStatus.Configured {
		event.EventState = db.ReverseEventStateDeferred
		event.DeferReason = "credentials_" + credStatus.Reason
		event.ErrorText = ""
		return p.db.AddReverseEvent(ctx, *event)
	}

	attemptedAt := time.Now().UTC()
	event.Attempts = 1
	event.LastAttemptAt = &attemptedAt

	switch event.Action {
	case db.ReverseActionUnfollow:
		if err := p.writer.DeleteRecord(ctx, cred, deleteATURI); err != nil {
			event.EventState = db.ReverseEventStateFailed
			event.ErrorText = err.Error()
			return p.db.AddReverseEvent(ctx, *event)
		}
		event.EventState = db.ReverseEventStatePublished
		publishedAt := time.Now().UTC()
		event.PublishedAt = &publishedAt
		event.ResultATURI = deleteATURI
		event.RawATJSON = fmt.Sprintf(`{"op":"delete","at_uri":%q}`, deleteATURI)
	default:
		created, err := p.writer.CreateRecord(ctx, cred, event.ResultCollection, record)
		if err != nil {
			event.EventState = db.ReverseEventStateFailed
			event.ErrorText = err.Error()
			return p.db.AddReverseEvent(ctx, *event)
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

	return p.db.AddReverseEvent(ctx, *event)
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

func (w *ATProtoReverseWriter) CreateRecord(ctx context.Context, cred reverseResolvedCredential, collection string, record any) (*ReverseCreatedRecord, error) {
	client, err := w.createSession(ctx, cred)
	if err != nil {
		return nil, err
	}
	resp, err := atproto.RepoCreateRecord(ctx, client, &atproto.RepoCreateRecord_Input{
		Collection: collection,
		Repo:       client.Auth.Did,
		Record:     &lexutil.LexiconTypeDecoder{Val: record},
	})
	if err != nil {
		return nil, fmt.Errorf("create record %s for %s: %w", collection, cred.DID, err)
	}
	rawRecordJSON, _ := json.Marshal(record)
	return &ReverseCreatedRecord{
		URI:           resp.Uri,
		CID:           resp.Cid,
		Collection:    collection,
		RawRecordJSON: string(rawRecordJSON),
	}, nil
}

func (w *ATProtoReverseWriter) DeleteRecord(ctx context.Context, cred reverseResolvedCredential, atURI string) error {
	client, err := w.createSession(ctx, cred)
	if err != nil {
		return err
	}
	parsed, err := syntax.ParseATURI(atURI)
	if err != nil {
		return fmt.Errorf("parse at uri %s: %w", atURI, err)
	}
	if parsed.Collection().String() == "" || parsed.RecordKey().String() == "" {
		return fmt.Errorf("at uri %s is missing collection or rkey", atURI)
	}
	_, err = atproto.RepoDeleteRecord(ctx, client, &atproto.RepoDeleteRecord_Input{
		Collection: parsed.Collection().String(),
		Repo:       client.Auth.Did,
		Rkey:       parsed.RecordKey().String(),
	})
	if err != nil {
		return fmt.Errorf("delete record %s for %s: %w", atURI, cred.DID, err)
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

func int64Ptr(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}
