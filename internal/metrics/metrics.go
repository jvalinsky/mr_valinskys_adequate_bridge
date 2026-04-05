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
)
