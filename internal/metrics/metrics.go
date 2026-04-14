package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MessagesPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_messages_published_total",
		Help: "Total number of messages successfully published to SSB",
	})

	MessagesFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_messages_failed_total",
		Help: "Total number of messages that failed to publish",
	})

	MessagesDeferred = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_messages_deferred_total",
		Help: "Total number of messages deferred due to missing dependencies",
	})

	DeferredExpired = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_deferred_expired_total",
		Help: "Total number of deferred messages expired (failed due to TTL)",
	})

	BlobsDownloaded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_blobs_downloaded_total",
		Help: "Total number of blobs downloaded from ATProto",
	})

	BlobsPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_blobs_published_total",
		Help: "Total number of blobs published to SSB",
	})

	ActiveAccounts = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_active_accounts",
		Help: "Number of currently active bridged accounts",
	})

	DeferredBacklog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_deferred_backlog",
		Help: "Number of messages currently deferred",
	})

	FirehoseLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_firehose_lag",
		Help: "Current firehose cursor lag (events behind)",
	})

	RateLimited = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_rate_limited_total",
		Help: "Total number of messages rate-limited per-DID",
	})

	IndexerQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_indexer_queue_depth",
		Help: "Current number of DIDs waiting in the indexer backfill/sync queue",
	})

	PublishDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "bridge_publish_duration_seconds",
		Help:    "Time spent in the SSB publish call",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})

	FirehoseReconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_firehose_reconnects_total",
		Help: "Total number of firehose WebSocket reconnection attempts",
	})

	DBSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_db_size_bytes",
		Help: "Size of the SQLite database file in bytes",
	})

	BlobStoreSizeBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_blob_store_size_bytes",
		Help: "Total size of the SSB blob store directory in bytes",
	})

	// DependencyFetches counts dependency resolution attempts by outcome and reason.
	// result label: "success", "error", "skip", "start"
	// reason label: the note field from the log event (e.g. "remote_fetch", "local_resolved", "already_visited")
	DependencyFetches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bridge_dependency_fetches_total",
		Help: "Total number of dependency resolution attempts, by result and reason",
	}, []string{"result", "reason"})

	DeferredOldestAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_deferred_oldest_age_seconds",
		Help: "Age in seconds of the oldest currently-deferred message (0 if backlog is empty)",
	})

	RetryExhaustedMessages = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_retry_exhausted_messages",
		Help: "Number of failed messages that have reached the maximum retry attempt count",
	})

	DeferredSchedulerPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_deferred_scheduler_published_total",
		Help: "Total messages published by the deferred resolver scheduler",
	})

	DeferredSchedulerFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_deferred_scheduler_failed_total",
		Help: "Total messages that failed during deferred resolver scheduler runs",
	})

	RetrySchedulerPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_retry_scheduler_published_total",
		Help: "Total messages published by the retry scheduler",
	})

	RetrySchedulerFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_retry_scheduler_failed_total",
		Help: "Total messages that failed during retry scheduler runs",
	})

	RoomMembers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_room_members",
		Help: "Number of members currently registered in the SSB room",
	})

	RoomAttendants = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_room_attendants",
		Help: "Number of peers currently connected to the SSB room",
	})

	RoomInvitesConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bridge_room_invites_consumed_total",
		Help: "Total number of room invite tokens consumed, by outcome",
	}, []string{"result"})

	RoomTunnelConnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_room_tunnel_connects_total",
		Help: "Total number of tunnel.connect attempts in the SSB room",
	})

	RoomTunnelAnnounceFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bridge_room_tunnel_announce_failures_total",
		Help: "Total number of tunnel.announce failures in the SSB room",
	})

	RoomMethodFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bridge_room_method_failures_total",
		Help: "Total number of room/tunnel method failures by method and reason",
	}, []string{"method", "reason", "error_kind"})

	RoomMembershipLookupFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bridge_room_membership_lookup_failures_total",
		Help: "Total number of membership lookup failures in room/tunnel methods",
	}, []string{"method", "error_kind"})

	RoomSQLiteBusyErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bridge_room_sqlite_busy_total",
		Help: "Total number of SQLITE_BUSY/locked errors seen in room/tunnel handlers",
	}, []string{"method"})
)
