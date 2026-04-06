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
)
