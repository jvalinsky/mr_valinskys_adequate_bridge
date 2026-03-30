# SSB Replication

This document covers how Secure Scuttlebutt peers exchange and synchronize data, including the legacy `createHistoryStream` mechanism and the more efficient Epidemic Broadcast Trees (EBT) protocol.

## Table of Contents

1. [Protocol Stack](#protocol-stack)
2. [Secret Handshake](#secret-handshake)
3. [Box Stream](#box-stream)
4. [RPC Protocol](#rpc-protocol)
5. [Classic Replication](#classic-replication)
6. [EBT Replication](#ebt-replication)
7. [See Also](#see-also)

---

## Protocol Stack

SSB uses a layered protocol architecture:

```
┌─────────────────────────────────────────────────────────────────┐
│                    SSB Protocol Stack                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                    Application Layer                        │  │
│   │           (Messages, Blobs, RPC calls)                   │  │
│   ├─────────────────────────────────────────────────────────┤  │
│   │                      RPC Protocol                         │  │
│   │        (MUXRPC: async, source, duplex streams)          │  │
│   ├─────────────────────────────────────────────────────────┤  │
│   │                    Box Stream                            │  │
│   │         (Authenticated encryption, streaming)               │  │
│   ├─────────────────────────────────────────────────────────┤  │
│   │                 Secret Handshake                          │  │
│   │        (Mutual auth + key exchange)                      │  │
│   ├─────────────────────────────────────────────────────────┤  │
│   │                  TCP Socket                              │  │
│   │              (Reliable transport)                        │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Secret Handshake

The Secret Handshake (SHS) establishes an authenticated encrypted connection between peers.

### Security Properties

- Mutual authentication via Ed25519 signatures
- Shared secret derived for encryption
- Protection against replay attacks
- Forward secrecy
- MITM resistance

### 4-Way Handshake Flow

```
┌─────────────────────────────────────────────────────────────────┐
│              Secret Handshake Protocol (SHS)                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Client                          Server                         │
│      │                                │                          │
│      │──── 1. ClientHello ──────────▶│                          │
│      │       (ephemeral_pk, HMAC)     │                          │
│      │                                │                          │
│      │◀─── 2. ServerHello ───────────│                          │
│      │       (ephemeral_pk, HMAC)    │                          │
│      │                                │                          │
│      │  Derive shared secrets:        │  Derive shared secrets:   │
│      │  • shared_secret_ab            │  • shared_secret_ab       │
│      │  • shared_secret_aB           │  • shared_secret_aB       │
│      │                                │                          │
│      │──── 3. ClientAuth ───────────▶│                          │
│      │       (signature, pk)          │                          │
│      │       encrypted               │                          │
│      │                                │                          │
│      │  Derive:                       │                          │
│      │  • shared_secret_Ab           │  Derive:                  │
│      │                                │  • shared_secret_Ab      │
│      │◀─── 4. ServerAccept ──────────│                          │
│      │       (signature)              │                          │
│      │       encrypted               │                          │
│      │                                │                          │
│      │◀═══════════════════════════════│                          │
│      │      Encrypted Box Stream       │                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Handshake Messages

| Message | Direction | Size | Purpose |
|---------|-----------|------|---------|
| ClientHello | C → S | 64B | Ephemeral key + HMAC |
| ServerHello | S → C | 64B | Ephemeral key + HMAC |
| ClientAuth | C → S | 112B | Signed auth + pk (encrypted) |
| ServerAccept | S → C | 80B | Server signature (encrypted) |

### Shared Secret Derivation

```
┌─────────────────────────────────────────────────────────────────┐
│                    Shared Secret Derivation                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Client computes:                                              │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ shared_secret_ab  = nacl_scalarmult(client_eph_sk,     │  │
│   │                                   server_eph_pk)       │  │
│   │                                                         │  │
│   │ shared_secret_aB  = nacl_scalarmult(client_eph_sk,     │  │
│   │                           pk_to_curve25519(server_pk)) │  │
│   │                                                         │  │
│   │ shared_secret_Ab  = nacl_scalarmult(                   │  │
│   │               sk_to_curve25519(client_sk),             │  │
│   │               server_eph_pk)                          │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   Final encryption key:                                         │
│   key = sha256(network_id + shared_secret_ab +                  │
│                shared_secret_aB + shared_secret_Ab)           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Box Stream

Box Stream provides authenticated encryption for the RPC layer.

### Message Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                    Box Stream Structure                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌──────────┬──────────┬──────────┬──────────┬──────────┐  │
│   │ Header   │  Body    │ Header   │  Body    │ Goodbye  │  │
│   │  34 B   │ 1-4096B  │  34 B   │ 1-4096B  │  34 B   │  │
│   └────┬─────┴────┬─────┴────┬─────┴────┬─────┴──────────┘  │
│        │          │         │          │                        │
│        ▼          ▼         ▼          ▼                        │
│   ┌─────────┐ ┌─────────┐                                           │
│   │ Enc hdr │ │ Enc body│  ...     ...                            │
│   │  tag(16)│ │  tag(16)│                                           │
│   │  len(2) │ │  data   │                                           │
│   │  +pad   │ │  +pad   │                                           │
│   └─────────┘ └─────────┘                                           │
│                                                                  │
│   Header: 16B tag + 2B length + 16B padding = 34 bytes          │
│   Body:    16B tag + variable + padding = 1-4096 bytes          │
│   Goodbye: 18 zero bytes, encrypted                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Box Stream Parameters

```
┌─────────────────────────────────────────────────────────────────┐
│                    Box Stream Key/Nonce Derivation                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Key derivation:                                               │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ key = sha256(network_id + ab + aB + Ab)                 │  │
│   │                                                         │  │
│   │ Where:                                                  │  │
│   │   ab  = shared_secret_ab (ephemeral-ephemeral)         │  │
│   │   aB  = shared_secret_aB (ephemeral-server)            │  │
│   │   Ab  = shared_secret_Ab (client-ephemeral)             │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   Two streams:                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Client→Server: key + server_pk_hash                     │  │
│   │ Server→Client: key + client_pk_hash                     │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   Starting nonces:                                              │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Client→Server: HMAC(network_id, server_eph_pk)[0:24]   │  │
│   │ Server→Client: HMAC(network_id, client_eph_pk)[0:24]   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   Nonces increment for each box in the stream                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## RPC Protocol

MUXRPC (Multiplexed RPC) enables multiple concurrent requests over a single connection.

### RPC Header (9 bytes)

```
┌─────────────────────────────────────────────────────────────────┐
│                    MUXRPC Message Format                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Flags (1B) │ Length (4B)      │ Request # (4B)        │   │
│   ├─────────────┼──────────────────┼───────────────────────┤   │
│   │ 0000SSCE    │  payload_length  │  request_number        │   │
│   │  ││││││││   │  (big-endian)   │  (signed, big-endian) │   │
│   │  ││││││││   │                  │                       │   │
│   │  ││││││││   └──────────────────┴───────────────────────┘   │
│   │  ││││││││                                                 │   │
│   │  ││││││││   S = Stream flag (1=part of stream)     │   │
│   │  ││││││││   C = End/Close flag (1=end or error)    │   │
│   │  ││││││││   E = Error flag (1=error)               │   │
│   │  ││││││││   TT = Body type (00=binary,01=JSON,10=?) │   │
│   │  └┬┴┴┴┴┴┘                                               │   │
│   │   └─ Must be zero                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### RPC Types

| Type | Description | Stream | End on Close |
|------|-------------|--------|--------------|
| `async` | Single request/response | No | Yes |
| `source` | Server streams responses | Yes | Yes |
| `sink` | Client streams requests | Yes | Yes |
| `duplex` | Bidirectional streaming | Yes | Yes |

### Request/Response Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    RPC Request/Response                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Source (streaming) request:                                    │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Request #N, Stream=1, End=0, Type=JSON                 │  │
│   │ {                                                     │  │
│   │   "name": ["createHistoryStream"],                    │  │
│   │   "type": "source",                                  │  │
│   │   "args": [{"id": "@feed..."}]                       │  │
│   │ }                                                     │  │
│   └─────────────────────────────────────────────────────────┘  │
│                            │                                    │
│                            ▼                                    │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Response #-N, Stream=1, End=0, Type=JSON              │  │
│   │ {msg: 1}                                              │  │
│   └─────────────────────────────────────────────────────────┘  │
│                            │                                    │
│                            ▼                                    │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Response #-N, Stream=1, End=0, Type=JSON              │  │
│   │ {msg: 2}                                              │  │
│   └─────────────────────────────────────────────────────────┘  │
│                            │                                    │
│                            ▼                                    │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Response #-N, Stream=1, End=1, Type=JSON              │  │
│   │ true                                                   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                            │                                    │
│                            ▼                                    │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Request #N, Stream=1, End=1, Type=JSON                │  │
│   │ true                                                   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Classic Replication

`createHistoryStream` is the original SSB replication mechanism.

### Replication Flow

```
┌─────────────────────────────────────────────────────────────────┐
│              Classic Replication Flow                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Peer A                           Peer B                         │
│      │                                │                          │
│      │──── whoami ──────────────────▶│                          │
│      │◀─── @feed.id ─────────────────│                          │
│      │                                │                          │
│      │──── createHistoryStream ──────▶│                          │
│      │       { id: @feed, seq: N }   │                          │
│      │                                │                          │
│      │◀──── [msg N+1] ───────────────│                          │
│      │◀──── [msg N+2] ───────────────│                          │
│      │◀──── [msg N+3] ───────────────│                          │
│      │◀──── ... ──────────────────────│                          │
│      │                                │                          │
│      │◀──── true (stream end) ───────│                          │
│      │──── true (close) ────────────▶│                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### createHistoryStream Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | string | Feed ID to replicate (required) |
| `sequence` | number | Start from this sequence |
| `limit` | number | Max messages to return |
| `live` | boolean | Stream new messages |
| `old` | boolean | Include existing messages |
| `keys` | boolean | Include message IDs/timestamps |

### Inefficiency

Classic replication requires:
1. Knowing what feeds exist
2. Requesting each feed individually
3. No efficient state comparison

---

## EBT Replication

Epidemic Broadcast Trees (EBT) provides efficient differential replication.

### Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    EBT Replication Overview                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Traditional (createHistoryStream):                            │
│   ┌────────┐     Request each feed     ┌────────┐               │
│   │ Peer A │◀─────────────────────────▶│ Peer B │               │
│   └────────┘     individually          └────────┘               │
│                                                                  │
│   EBT (differential replication):                                │
│   ┌────────┐     Vector clocks        ┌────────┐               │
│   │ Peer A │◀════════════════════════▶│ Peer B │               │
│   └────────┘     + deltas only       └────────┘               │
│                                                                  │
│   EBT exchanges:                                                 │
│   • What each peer HAS (their sequence numbers)                  │
│   • Only sends messages the peer is missing                      │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### EBT Session Initialization

```
┌─────────────────────────────────────────────────────────────────┐
│                    EBT Session Establishment                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Client (requester)              Server (responder)           │
│      │                                │                          │
│      │──── ebt.replicate ──────────▶│                          │
│      │       {version: 3,           │                          │
│      │        format: "classic"}     │                          │
│      │       type: "duplex"         │                          │
│      │                                │                          │
│      │◀─── Vector Clock ─────────────│                          │
│      │       {@feed1: 450,          │                          │
│      │        @feed2: 12,           │                          │
│      │        @feed3: -1}           │                          │
│      │                                │                          │
│      │◀═══════════════════════════════│                          │
│      │     Duplex message exchange    │                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Vector Clocks

EBT uses vector clocks (called "notes") to track what each peer has.

```
┌─────────────────────────────────────────────────────────────────┐
│                Vector Clock Encoding                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Each feed maps to a signed integer encoding:                    │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │  Binary representation:  [replicate_flag][sequence]      │   │
│   │                                                          │   │
│   │  Bit 0 (LSB):    receive flag (0=true, 1=false)       │   │
│   │  Bits 1+:        sequence number (arithmetic right shift)│   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Encoding formula:                                               │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  if not replicating: value = -1                         │  │
│   │  else: value = (sequence << 1) | receive_flag           │  │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                  │
│   Decoding formula:                                              │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │  if value < 0: not replicating                          │  │
│   │  else:                                                   │  │
│   │    receive_flag = value & 1                              │  │
│   │    sequence = value >> 1                                 │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Vector Clock Value Examples

| Encoded | Replicate | Rx Flag | Sequence | Notes |
|---------|-----------|---------|----------|-------|
| `-1` | false | - | - | Peer doesn't want this feed |
| `0` | true | true | 0 | Peer has no messages |
| `1` | true | false | 0 | Peer has no messages |
| `2` | true | true | 1 | Peer has 1 message |
| `3` | true | false | 1 | Peer has 1 message |
| `450` | true | true | 225 | Peer has 225 messages |

### EBT Message Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    EBT Differential Sync                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Peer A                           Peer B                         │
│      │                                │                          │
│      │──── Initial state ────────────▶│                          │
│      │  {feed1: 10, feed2: 5}        │                          │
│      │                                │                          │
│      │◀─── Peer B state ─────────────│                          │
│      │  {feed1: 8, feed2: 10}        │                          │
│      │                                │                          │
│      │  Compute diffs:                │  Compute diffs:          │
│      │  • feed1: A has 10, B has 8   │  • feed1: B has 8, A has │
│      │    → send msgs 9, 10 to B      │    10 → request from A   │
│      │  • feed2: A has 5, B has 10   │  • feed2: B has 10      │
│      │    → request msgs 6-10 from B  │    → already synced     │
│      │                                │                          │
│      │◀──── [msg 6-10 from feed2] ───│                          │
│      │                                │                          │
│      │──── [msg 9-10 from feed1] ────▶│                          │
│      │                                │                          │
│      │◀─── State update ──────────────│                          │
│      │  {feed2: 10}                  │                          │
│      │                                │                          │
│      │──── State update ────────────▶│                          │
│      │  {feed1: 10}                  │                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### EBT RPC Format

```json
// Vector clock (control message)
{
  "@feed1.ed25519": 450,
  "@feed2.ed25519": 12,
  "@feed3.ed25519": -1
}

// Feed message
{
  "previous": "%prevMsgId.sha256",
  "author": "@authorFeed.ed25519",
  "sequence": 2,
  "timestamp": 1514517078157,
  "hash": "sha256",
  "content": {
    "type": "post",
    "text": "Hello world!"
  }
}
```

---

## Bridge Implementation Examples

### EBT Handler Registration (Go)

From `internal/ssb/sbot/ebt_handler.go`:

```go
func (s *Sbot) registerEBTHandler() {
    s.handlerMux.Register(
        ebt.MethodReplicate,
        muxrpc.Method{
            Namespace: "ebt",
            Name:     "replicate",
        },
        s.handleEBTReplicate,
    )
}

func (s *Sbot) handleEBTReplicate(
    ctx context.Context,
    req *muxrpc.Request,
    send func(args interface{}) error,
) error {
    duplex := ebt.NewDuplex(
        req.Sink(),
        req.Source(),
        s.stateMatrix,
        s.feedManager,
        ebt.Opts{
            Format: ebt.FormatClassic,
            Self:   s.keyPair.FeedRef(),
        },
    )
    
    return duplex.HandleDuplex(ctx)
}
```

### State Matrix (Go)

From `internal/ssb/replication/ebt.go`:

```go
type StateMatrix struct {
    mu      sync.Mutex
    peers   map[string]*PeerState  // peer feed ref -> state
    selfSeq map[string]int64       // feed ref -> sequence
}

type PeerState struct {
    Clock map[string]int64  // feed ref -> sequence
}

// Changed computes differential updates between peers
func (sm *StateMatrix) Changed(self, peer *refs.FeedRef) (ebt.NetworkFrontier, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    
    // Get self's feeds and sequences
    selfFeeds := sm.getSelfFeeds()
    
    // Get peer's feeds and sequences
    peerClock := sm.getPeerClock(peer)
    
    // Compute which feeds need updates
    var frontier ebt.NetworkFrontier
    for feed, selfSeq := range selfFeeds {
        peerSeq := peerClock[feed]
        if selfSeq > peerSeq {
            // We have messages peer is missing
            frontier[feed] = selfSeq
        }
    }
    
    return frontier, nil
}
```

### Feed Manager Adapter (Go)

From `internal/ssb/sbot/feed_manager_adapter.go`:

```go
type FeedManagerAdapter struct {
    repo  *margeraret.RepoLog
}

func (fma *FeedManagerAdapter) GetMessage(seq int64) (map[string]interface{}, error) {
    msg, err := fma.repo.Get(seq)
    if err != nil {
        return nil, err
    }
    
    // Classic format for EBT replication
    // Fields MUST be in specific order for TF compatibility
    return map[string]interface{}{
        "previous":  msg.Previous,
        "author":   msg.Author,
        "sequence": msg.Sequence,
        "timestamp": msg.Timestamp,
        "hash":     "sha256",
        "content":  msg.Content,
        "signature": base64Sig + ".sig.ed25519",
    }, nil
}
```

---

## See Also

- **[SSB Protocol Fundamentals](ssb-protocol-fundamentals.md)** - Identity, feeds, messages, signing
- **[SSB Rooms](ssb-rooms.md)** - Room2 server protocol and tunnel connections
- **[EBT Replication Debugging](../ebt-replication.md)** - Bridge-specific EBT implementation notes
- [EBT Reference Implementation](https://github.com/ssbc/epidemic-broadcast-trees)
- [ssb-ebt](https://github.com/ssbc/ssb-ebt)
- [Planetary EBT Documentation](https://dev.planetary.social/replication/ebt.html)
- [SIP-007: Rooms 2](https://github.com/ssbc/sips/blob/master/007.md)
