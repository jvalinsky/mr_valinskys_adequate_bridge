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
)
