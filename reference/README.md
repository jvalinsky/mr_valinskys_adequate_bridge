# Reference Materials

External project sources used as architectural reference during development of the bridge. These are vendored snapshots for offline study — not runtime dependencies.

## Core Protocol References

| Directory | Description | Upstream |
|-----------|-------------|----------|
| `go-ssb` | Go implementation of the SSB protocol (sbot, feeds, replication) | github.com/ssbc/go-ssb |
| `go-ssb-room` | SSB Room v1+v2 server in Go | github.com/ssbc/go-ssb-room |
| `scuttlego` | Planetary's Go SSB implementation (alternative architecture) | github.com/planetary-social/scuttlego |
| `sips` | SSB Implementation Protocols — RFC-style protocol specs | github.com/ssbc/sips |
| `scuttlebutt-protocol-guide` | Illustrated guide to the SSB protocol | github.com/ssbc/scuttlebutt-protocol-guide |

## SSB Libraries

| Directory | Description | Upstream |
|-----------|-------------|----------|
| `go-muxrpc` | Go implementation of the MUXRPC protocol | github.com/ssbc/go-muxrpc |
| `go-secretstream` | Go implementation of SSB secret-handshake + boxstream | github.com/ssbc/go-secretstream |
| `ssb-db2` | SSB database layer (JavaScript, log-based) | github.com/ssbc/ssb-db2 |
| `ssb-uri-rs` | SSB URI parsing and generation (Rust) | github.com/ssbc/ssb-uri-rs |
| `jitdb` | Just-in-time indexing database for SSB (JavaScript) | github.com/ssbc/jitdb |

## Bridge & Application References

| Directory | Description | Upstream |
|-----------|-------------|----------|
| `bridgy-fed` | Cross-protocol bridge (ATProto, ActivityPub, IndieWeb) | github.com/snarfed/bridgy-fed |
| `tildefriends` | SSB client + room platform with web interface | dev.tildefriends.net/cory/tildefriends |
| `planetary-ios` | Planetary SSB iOS client | github.com/planetary-social/planetary-ios |
| `planetary-graphql` | GraphQL server complementing go-ssb-room | github.com/planetary-social/planetary-graphql |
| `rooms-frontend` | Vue.js frontend for go-ssb-room + planetary-graphql | github.com/planetary-social/rooms-frontend |
