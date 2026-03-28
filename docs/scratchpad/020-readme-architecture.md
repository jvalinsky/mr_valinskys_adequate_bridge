# 020 README Section: Architecture

## Data Flow

```
ATProto Firehose (subscribeRepos)
        │
        ▼
   ┌─────────┐     ┌────────┐     ┌────────────┐     ┌──────┐
   │ firehose │────▶│ bridge │────▶│ ssbruntime │────▶│  db  │
   └─────────┘     └────────┘     └────────────┘     └──────┘
                       │                 │
                       ▼                 │
                  ┌────────┐             │
                  │ mapper │             │
                  └────────┘             │
                       │                 │
                       ▼                 ▼
                  ┌────────────┐    ┌──────┐
                  │ blobbridge │    │ room │
                  └────────────┘    └──────┘
```

The **firehose** package streams ATProto commits. The **bridge** processor coordinates record processing: it invokes the **mapper** to translate ATProto records into SSB payloads, the **blobbridge** to mirror media, and the **ssbruntime** to publish messages to SSB feeds. The **room** package runs an embedded Room2 server for SSB peer connectivity. All state is persisted to SQLite via the **db** package.

## Internal Packages

| Package | Description |
|---------|-------------|
| `bridge` | Coordinates firehose ingestion, mapping, publishing, and persistence |
| `db` | SQLite-backed persistence for bridge state and mappings |
| `ssbruntime` | Manages local SSB storage and publishing dependencies |
| `firehose` | Streams ATProto repository commits from subscribeRepos |
| `mapper` | Converts supported ATProto records into SSB-compatible payloads |
| `bots` | Manages deterministic SSB identities derived from ATProto DIDs |
| `blobbridge` | Mirrors ATProto blobs into the local SSB blob store |
| `backfill` | Replays supported records from ATProto repositories via sync.getRepo |
| `publishqueue` | Per-DID publish ordering with bounded worker parallelism |
| `room` | Embeds and supervises the go-ssb-room runtime |
| `web/handlers` | HTTP routes for the bridge admin UI |
| `web/templates` | HTML views for the bridge admin UI |
| `web/security` | HTTP middleware for admin UI exposure hardening |
| `presentation` | Formats bridge data for human-readable CLI and UI output |
| `livee2e` | End-to-end tests against a live ATProto relay and Room2 instance |
| `smoke` | Deterministic integration tests for the bridge pipeline |
