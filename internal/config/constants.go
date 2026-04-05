package config

const (
	DefaultPageLimit      = 100
	DefaultMessageLimit   = 50
	DefaultBlobLimit      = 50
	DefaultAccountLimit   = 50
	DefaultPublishedLimit = 20
	DefaultFailureLimit   = 50
	MaxPageLimit          = 500
	MaxRetryLimit         = 500

	MaxDependencyDepth = 16
	MaxRetries         = 50

	HealthCheckTimeout = 60

	DefaultATProtoSourceKey = "relay"

	BlobMaxAgeDays = 90
	BlobMaxSizeGB  = 50
	BlobGCInterval = 24

	DeferredMaxAgeDays          = 2 // 48h before expired deferred messages are failed
	DeferredExpiryIntervalHours = 6 // hours between expiry scheduler runs

	MaxMessagesPerDIDPerMinute = 300 // 5 msg/sec per DID; 0 disables rate limiting
	RateLimiterCleanupInterval = 5   // minutes between stale limiter cleanup
	RateLimiterIdleTimeout     = 10  // minutes before a DID's limiter is removed
)
