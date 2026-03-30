# SSB Rooms

This document covers SSB Room2 servers, which provide relay services for peers behind NAT/firewalls and enable the `tunnel.connect` mechanism for indirect peer connections.

## Table of Contents

1. [Overview](#overview)
2. [Room Architecture](#room-architecture)
3. [Privacy Modes](#privacy-modes)
4. [Tunnel Connections](#tunnel-connections)
5. [Tunnel Authentication](#tunnel-authentication)
6. [Room Metadata](#room-metadata)
7. [Room MUXRPC APIs](#room-muxrpc-apis)
8. [See Also](#see-also)

---

## Overview

Room servers are SSB peers with public internet presence that act as intermediaries for peers behind NAT/firewalls.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Room Server Role                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Traditional:                                                  │
│   ┌────────┐              ┌────────┐                         │
│   │ Peer A │──────✗──────│ Peer B │                         │
│   │ (NAT)  │   blocked   │ (NAT)  │                         │
│   └────────┘              └────────┘                         │
│                                                                  │
│   With Room:                                                   │
│   ┌────────┐    ┌────────┐    ┌────────┐                     │
│   │ Peer A │───▶│ Room M │◀───│ Peer B │                     │
│   │ (NAT)  │◀───│        │───▶│ (NAT)  │                     │
│   └────────┘    └────────┘    └────────┘                     │
│                      │                                        │
│                      ▼                                        │
│               Public IP Address                                 │
│               (reachable)                                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Room Capabilities

| Capability | Description |
|------------|-------------|
| Public MUXRPC | Peers can connect to room's MUXRPC endpoint |
| Tunnel Relay | Peers can connect through room to reach other peers |
| Member Registry | Track which feeds are "internal users" |
| Alias System | Allow users to register human-readable names |
| Privacy Modes | Control who can join the room |

---

## Room Architecture

### Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    Room Server Components                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                    Room Server                             │  │
│   │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │  │
│   │  │   MUXRPC    │  │   Tunnel    │  │   Alias     │   │  │
│   │  │   Handler   │  │   Relay    │  │   Manager   │   │  │
│   │  └─────────────┘  └─────────────┘  └─────────────┘   │  │
│   │         │                │                │             │  │
│   │         └────────────────┼────────────────┘             │  │
│   │                          │                              │  │
│   │                   ┌─────┴─────┐                       │  │
│   │                   │  Internal  │                       │  │
│   │                   │   User    │                       │  │
│   │                   │  Registry │                       │  │
│   │                   └───────────┘                       │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### External vs Internal Users

| Type | Description |
|------|-------------|
| External User | Connected to room but not registered; can only consume aliases |
| Internal User | Registered member; has tunnel address; can be reached via tunnels |

### Getting a Tunnel Address

A peer becomes an internal user when:
- **Open mode**: Uses an invite code
- **Community mode**: Gets invite from internal user
- **Restricted mode**: Gets invite from moderator

---

## Privacy Modes

Rooms support three privacy modes controlling who can join:

```
┌─────────────────────────────────────────────────────────────────┐
│                       Room Privacy Modes                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                        OPEN                                 │  │
│   │   • Invite codes are public                              │  │
│   │   • Anyone can join                                     │  │
│   │   • No authentication required                           │  │
│   │   • Similar to SSB Room v1                              │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                      COMMUNITY                             │  │
│   │   • Only members can invite                             │  │
│   │   • Requires web dashboard login                        │  │
│   │   • Members create invite codes                         │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                      RESTRICTED                           │  │
│   │   • Only moderators can invite                          │  │
│   │   • Aliases not supported                              │  │
│   │   • Strictest access control                            │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Joining Flow by Mode

#### Open Mode
```
1. External user gets invite code (from room website)
2. External user consumes invite code
3. Room grants tunnel address
4. User becomes internal user
```

#### Community Mode
```
1. Internal user logs into web dashboard
2. Internal user creates invite code
3. Internal user sends invite to external user
4. External user consumes invite code
5. External user becomes internal user
```

#### Restricted Mode
```
1. Moderator logs into web dashboard
2. Moderator creates invite code
3. Moderator sends invite to external user
4. External user consumes invite code
5. External user becomes internal user
```

---

## Tunnel Connections

Tunnels enable peers behind NAT to communicate through the room server.

### Tunnel Connection Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                  Tunnel Connection Establishment                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Peer A                    Room M                    Peer B         │
│      │                        │                          │           │
│      │════ TCP + SHS ════════╪════════════════════════════╪═══     │
│      │   (outer conn)         │                        │           │
│      │                        │                        │           │
│      │◀═══════════════════════╪════════════════════════╪═══════     │
│      │   (bidirectional)      │                        │           │
│      │                        │                        │           │
│      │                        │════ TCP + SHS ═════════╪══════     │
│      │                        │   (outer conn)         │           │
│      │                        │                        │           │
│      │◀═════ tunnel.connect ─╪════════════════════════╪═══════     │
│      │       (request A→B)   │                        │           │
│      │                        │                        │           │
│      │════ tunnel.connect ════╪════════════════════════╪═══════     │
│      │       (to B)          │                        │           │
│      │                        │                        │           │
│      │◀═══════════════════════════════════════════════════════│     │
│      │   Inner encrypted tunnel (A ↔ B)                   │     │
│      │   Room cannot read payload (double encrypted)        │     │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Tunnel Address Format

Tunnel addresses follow the multiserver-address format:

```
tunnel:ROOM_FEED_ID:TARGET_FEED_ID
```

Example:
```
tunnel:@7MG1hyfz8SsxlIgansud4LKM57IHIw2Okw
/hvOdeJWw=.ed25519:@1b9KP8znF7A4i8wnSevBSK
2ZabI/Re4bYF/Vh3hXasQ=.ed25519
```

### Tunnel vs Direct Connections

```
┌─────────────────────────────────────────────────────────────────┐
│                  Connection Types Comparison                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Direct Connection:                                             │
│   ┌────────┐              ┌────────┐                            │
│   │ Peer A │══════════════│ Peer B │                            │
│   └────────┘    TCP+TLS   └────────┘                            │
│        │                    │                                   │
│        └────────────────────┘                                   │
│           Single encryption layer                                │
│                                                                  │
│   Tunneled Connection:                                           │
│   ┌────────┐    ┌────────┐    ┌────────┐                      │
│   │ Peer A │───▶│ Room M │───▶│ Peer B │                      │
│   └────────┘    └────────┘    └────────┘                      │
│        │          │            │                               │
│        └──────────┴────────────┘                               │
│           Double encryption (room sees outer only)               │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Double Encryption

Tunneled connections are encrypted twice:
1. **Outer encryption**: Between peer and room (standard SHS)
2. **Inner encryption**: Between the two peers (via tunnel)

The room server can see:
- Who is connecting
- Connection metadata
- Outer encrypted traffic

The room server **cannot** see:
- Inner tunnel contents
- Actual messages exchanged

---

## Tunnel Authentication

Tunnel authentication ensures peers only receive connections from followed accounts.

### Authentication Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Tunnel Authentication                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   When Peer B requests tunnel to Peer A:                        │
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │                    Room M checks:                         │  │
│   │                                                          │  │
│   │   1. Does A follow B?                                   │  │
│   │      └── YES → Allow tunnel                              │  │
│   │      └── NO  → Check if B follows A                      │  │
│   │                                                          │  │
│   │   2. Does B follow A?                                   │  │
│   │      └── YES → Allow tunnel (mutual)                     │  │
│   │      └── NO  → Reject connection                         │  │
│   │                                                          │  │
│   │   ⚠ Tunnel authentication requires MUTUAL follows        │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Mutual Follow Requirement

```
┌─────────────────────────────────────────────────────────────────┐
│                    Mutual Follow Requirement                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Allowed:                                                       │
│   ┌────────┐                          ┌────────┐               │
│   │ A follows B │ ◄────────────────► │ B follows A │          │
│   └────────┘      (mutual follow)      └────────┘               │
│                                                                  │
│   Blocked:                                                      │
│   ┌────────┐      ┌────────┐                                 │
│   │ A follows B │      │ B does NOT follow A │               │
│   └────┬─────┘      └────────┘                                 │
│        │                                                          │
│        └──────────────────✗── Tunnel rejected                │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Room Metadata

Peers can query room metadata to learn about capabilities.

### room.metadata MUXRPC API

```json
// Request: async, no args
{}

// Response:
{
  "name": "my-ssb-room",
  "membership": true,
  "features": [
    "tunnel",
    "room2",
    "alias",
    "httpAuth",
    "httpInvite"
  ]
}
```

### Feature Flags

| Feature | Description |
|---------|-------------|
| `tunnel` | Room supports tunnel connections |
| `room1` | Room is compatible with Room v1 (Open mode) |
| `room2` | Room supports Room v2 features |
| `alias` | Room supports alias registration |
| `httpAuth` | Room supports HTTP authentication |
| `httpInvite` | Room supports HTTP invites |

---

## Room MUXRPC APIs

### Core APIs

| API | Type | Description |
|-----|------|------------|
| `room.metadata` | async | Get room information |
| `room.attendants` | source | Stream of members joining/leaving |
| `room.registerAlias` | async | Register a room alias |
| `room.revokeAlias` | async | Remove an alias |

### Tunnel APIs

| API | Type | Description |
|-----|------|-------------|
| `tunnel.connect` | duplex | Establish tunnel to another peer |
| `tunnel.isRoom` | async | Check if address is a room |

### Alias Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Alias Registration Flow                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   User (internal)                Room Server                       │
│      │                            │                              │
│      │──── room.registerAlias ───▶│                              │
│      │       (alias, signature)  │                              │
│      │                            │                              │
│      │     Check:                │                              │
│      │     • Valid alias format? │                              │
│      │     • Not already taken? │                              │
│      │     • Signature valid?   │                              │
│      │                            │                              │
│      │◀─── Success: URL ────────│                              │
│      │       alice.room.example  │                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Bridge Implementation Examples

### Room Runtime (Go)

From `internal/room/runtime.go`:

```go
type Runtime struct {
    srv    *roomsrv.Server
    muxRPC *muxrpc.Handler
    httpSrv *http.Server
}

func NewRuntime(ctx context.Context, opts Options) (*Runtime, error) {
    srv := roomsrv.New(roomsrv.Options{
        KeyPair: opts.KeyPair,
        ListenAddr: opts.ListenAddr,
    })
    
    // Register tunnel handler
    srv.Register(...)
    
    return &Runtime{
        srv:    srv,
        muxRPC: srv.MuxRPCHandler(),
    }, nil
}
```

### Tunnel Handler (Go)

From `internal/ssb/muxrpc/handlers/room/tunnel.go`:

```go
func (h *TunnelHandler) handleConnect(
    ctx context.Context,
    req *muxrpc.Request,
    addr *refs.FeedRef,
) error {
    // 1. Look up peer's tunnel address from registry
    tunnelAddr := h.registry.GetTunnelAddress(addr)
    if tunnelAddr == nil {
        return fmt.Errorf("peer not in room")
    }
    
    // 2. Establish connection to target peer through room
    conn, err := h.room.DialTunnel(ctx, addr)
    if err != nil {
        return fmt.Errorf("tunnel dial failed: %w", err)
    }
    
    // 3. Start secret handshake with target peer
    shsConn, err := h.handshaker.Client(ctx, conn)
    if err != nil {
        return fmt.Errorf("handshake failed: %w", err)
    }
    
    // 4. Wrap in box stream for encryption
    boxStream := secretstream.New(shsConn, h.keyPair)
    
    // 5. Start MUXRPC on the tunnel connection
    return h.serveMuxRPC(ctx, boxStream)
}
```

### Room Member Announce (Go)

```go
// When a peer connects to the room
func (s *RoomServer) OnConnect(ctx context.Context, conn net.Conn, pk refs.FeedRef) {
    // Check if peer is internal user
    if s.isInternalUser(pk) {
        // Grant tunnel address
        s.registry.GrantTunnelAddress(pk)
        
        // Broadcast to attendants stream
        s.attendants.Broadcast(AttendantJoined{
            ID: pk,
        })
    }
}
```

---

## See Also

- **[SSB Protocol Fundamentals](ssb-protocol-fundamentals.md)** - Identity, feeds, messages, signing
- **[SSB Replication](ssb-replication.md)** - EBT and createHistoryStream protocols
- **[EBT Replication Debugging](../ebt-replication.md)** - Bridge-specific EBT implementation notes
- [SIP-007: Rooms 2](https://github.com/ssbc/sips/blob/master/007.md)
- [Room Server Implementation](https://github.com/ssbc/ssb-room-server)
- [go-ssb-room](https://github.com/ssbc/go-ssb-room)
