# SSB Bot Manager

- Implemented `internal/bots/manager.go`
- Resolves ATProto DIDs to deterministic SSB feeds using HMAC-SHA256 with a master seed.
- Keeps a pool of active `ssb.Publisher` instances (`map[string]ssb.Publisher`).
- The `GetPublisher` function creates an SSB publisher tied to the derived `botKeyPair` using `go.cryptoscope.co/ssb/message.OpenPublishLog`.
- Wrote tests to ensure `deriveKeyPair` deterministically generates the same SSB `FeedRef` and private key for a given DID, but generates different keys for different DIDs.