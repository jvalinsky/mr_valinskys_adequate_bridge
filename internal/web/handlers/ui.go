// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Database defines the persistence surface required by UI handlers.
type Database interface {
	CheckBridgeHealth(ctx context.Context, maxStale time.Duration) (*db.BridgeHealthStatus, error)
	CountBridgedAccounts(ctx context.Context) (int, error)
	CountMessages(ctx context.Context) (int, error)
	CountPublishedMessages(ctx context.Context) (int, error)
	CountPublishFailures(ctx context.Context) (int, error)
	CountDeferredMessages(ctx context.Context) (int, error)
	CountDeletedMessages(ctx context.Context) (int, error)
	CountBlobs(ctx context.Context) (int, error)
	GetBridgeState(ctx context.Context, key string) (string, bool, error)
	ListTopDeferredReasons(ctx context.Context, limit int) ([]db.DeferredReasonCount, error)
	ListTopIssueAccounts(ctx context.Context, limit int) ([]db.AccountIssueSummary, error)
	ListBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error)
	ListMessagesPage(ctx context.Context, query db.MessageListQuery) (db.MessagePage, error)
	ListMessageTypes(ctx context.Context) ([]string, error)
	GetMessage(ctx context.Context, atURI string) (*db.Message, error)
	ListThread(ctx context.Context, rootURI string) ([]db.Message, error)
	GetPublishFailures(ctx context.Context, limit int) ([]db.Message, error)
	GetRecentBlobs(ctx context.Context, limit int) ([]db.Blob, error)
	GetAllBridgeState(ctx context.Context) ([]db.BridgeState, error)
	GetKnownPeers(ctx context.Context) ([]db.KnownPeer, error)
	AddKnownPeer(ctx context.Context, p db.KnownPeer) error
	ResetMessageForRetry(ctx context.Context, atURI string) error
	GetBlobBySSBRef(ctx context.Context, ssbBlobRef string) (*db.Blob, error)
	GetBlob(ctx context.Context, atCID string) (*db.Blob, error)
	GetLatestDeferredReason(ctx context.Context) (string, bool, error)
	GetLatestATProtoEventCursor(ctx context.Context) (int64, bool, error)
	GetATProtoSource(ctx context.Context, sourceKey string) (*db.ATProtoSource, error)
	GetBridgedAccount(ctx context.Context, atDID string) (*db.BridgedAccount, error)
	ListPublishedMessagesGlobal(ctx context.Context, limit int) ([]db.Message, error)
	AddBridgedAccount(ctx context.Context, acc db.BridgedAccount) error
}

// BlobStore defines the blob retrieval surface for the UI.
type BlobStore interface {
	Get(hash []byte) (io.ReadCloser, error)
}

// SSBStatusProvider provides real-time status of the internal SSB node.
type SSBStatusProvider interface {
	GetPeers() []PeerStatus
	GetEBTState() map[string]map[string]int64
	ConnectPeer(ctx context.Context, addr string, pubKey []byte) error
}

// ATProtoDebugStore provides direct read access to indexer state persisted in SQLite.
type ATProtoDebugStore interface {
	GetATProtoSource(ctx context.Context, sourceKey string) (*db.ATProtoSource, error)
	ListTrackedATProtoRepos(ctx context.Context, state string) ([]db.ATProtoRepo, error)
	ListATProtoEventsAfter(ctx context.Context, cursor int64, limit int) ([]db.ATProtoRecordEvent, error)
}

// ATProtoService exposes the in-process indexer API used by debug routes.
type ATProtoService interface {
	TrackRepo(ctx context.Context, did, reason string) error
	UntrackRepo(ctx context.Context, did string) error
	GetRepoInfo(ctx context.Context, did string) (*db.ATProtoRepo, error)
	GetRecord(ctx context.Context, atURI string) (*db.ATProtoRecord, error)
	ListRecords(ctx context.Context, did, collection, cursor string, limit int) ([]db.ATProtoRecord, error)
}

type PeerStatus struct {
	Addr       string
	Feed       string
	ReadBytes  int64
	WriteBytes int64
	Latency    time.Duration
}

// UIHandler serves admin pages backed by bridge database state.
type UIHandler struct {
	db           Database
	logger       *log.Logger
	atpClient    PDSClientInterface
	blobStore    BlobStore
	ssbStatus    SSBStatusProvider
	atprotoStore ATProtoDebugStore
	atprotoSvc   ATProtoService
	roomOps      RoomOpsProvider
}

// NewUIHandler creates a UIHandler bound to database.
func NewUIHandler(database Database, logger *log.Logger, atpClient PDSClientInterface, blobStore BlobStore, ssbStatus SSBStatusProvider) *UIHandler {
	return &UIHandler{
		db:        database,
		logger:    logutil.Ensure(logger),
		atpClient: atpClient,
		blobStore: blobStore,
		ssbStatus: ssbStatus,
	}
}

// WithATProto attaches ATProto debug and service surfaces to the UI handler.
func (h *UIHandler) WithATProto(store ATProtoDebugStore, service ATProtoService) *UIHandler {
	h.atprotoStore = store
	h.atprotoSvc = service
	return h
}

// WithRoomOps attaches room operations provider to the admin UI handler.
func (h *UIHandler) WithRoomOps(provider RoomOpsProvider) *UIHandler {
	h.roomOps = provider
	return h
}

// Mount registers admin UI routes on r.
func (h *UIHandler) Mount(r chi.Router) {
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/healthz", h.handleHealthz)
	r.Get("/", h.handleDashboard)
	r.Get("/events", h.handleEvents)
	r.Get("/accounts", h.handleAccounts)
	r.Post("/accounts", h.handleAccountsAdd)
	r.Get("/messages", h.handleMessages)
	r.Get("/messages/detail", h.handleMessageDetail)
	r.Get("/messages/thread", h.handleMessageThread)
	r.Get("/failures", h.handleFailures)
	r.Post("/failures/retry", h.handleFailuresRetry)
	r.Get("/blobs", h.handleBlobs)
	r.Get("/state", h.handleState)
	r.Post("/messages/retry", h.handleMessageRetry)
	r.Get("/post", h.handlePost)
	r.Post("/post", h.handlePostAction)
	r.Get("/feed", h.handleFeed)
	r.Get("/blobs/view", h.handleBlobView)
	r.Get("/connections", h.handleConnections)
	r.Post("/connections/add", h.handleConnectionAdd)
	r.Post("/connections/connect", h.handleConnectionConnect)
	r.Get("/room", h.handleRoomOverview)
	r.Get("/room/members", h.handleRoomMembersRoles)
	r.Get("/room/attendants", h.handleRoomAttendantsTunnels)
	r.Get("/room/aliases", h.handleRoomAliasesInvites)
	r.Get("/room/moderation", h.handleRoomModeration)
	r.Post("/room/members/role", h.handleRoomMemberRoleSet)
	r.Post("/room/members/remove", h.handleRoomMemberRemove)
	r.Post("/room/invites/create", h.handleRoomInviteCreate)
	r.Post("/room/invites/revoke", h.handleRoomInviteRevoke)
	r.Post("/room/aliases/revoke", h.handleRoomAliasRevoke)
	r.Post("/room/denied/add", h.handleRoomDeniedAdd)
	r.Post("/room/denied/remove", h.handleRoomDeniedRemove)
	r.Route("/api/atproto", func(r chi.Router) {
		r.Get("/health", h.handleATProtoHealth)
		r.Get("/source", h.handleATProtoSource)
		r.Get("/repo", h.handleATProtoRepo)
		r.Get("/repos", h.handleATProtoRepos)
		r.Post("/repos/track", h.handleATProtoTrackRepo)
		r.Post("/repos/untrack", h.handleATProtoUntrackRepo)
		r.Get("/record", h.handleATProtoRecord)
		r.Get("/records", h.handleATProtoRecords)
		r.Get("/events", h.handleATProtoEvents)
	})
}



