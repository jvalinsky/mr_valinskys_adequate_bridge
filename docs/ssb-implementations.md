# SSB Implementation Reference

This document provides code examples from the bridge implementation, demonstrating how to implement key SSB protocol concepts in Go.

## Table of Contents

1. [Message Signing](#message-signing)
2. [Signature Verification](#signature-verification)
3. [Feed Manager for EBT](#feed-manager-for-ebt)
4. [MUXRPC Handler](#muxrpc-handler)
5. [Box Stream](#box-stream)
6. [Secret Handshake](#secret-handshake)
7. [Works Cited](#works-cited)

---

## Message Signing

### Canonical JSON Marshaling

From `internal/ssb/message/legacy/sign.go`:

```go
package legacy

import (
    "bytes"
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
    "strconv"
)

// Message represents an SSB classic message
type Message struct {
    Previous  *refs.MessageRef
    Author   refs.FeedRef
    Sequence int64
    Timestamp int64
    Hash     string
    Content  interface{}
    Signature string
}

// marshalForSigning creates canonical JSON for signing.
// The field order MUST match TF expectations.
func (m *Message) marshalForSigning() ([]byte, error) {
    buf := &bytes.Buffer{}
    buf.WriteString("{\n")
    
    // Field 1: previous
    buf.WriteString(`  "previous": `)
    if m.Previous != nil {
        buf.WriteString(`"` + m.Previous.String() + `"`)
    } else {
        buf.WriteString("null")
    }
    buf.WriteString(",\n")
    
    // Field 2: author
    buf.WriteString(`  "author": "` + m.Author.String() + `",\n`)
    
    // Field 3: sequence
    buf.WriteString(`  "sequence": `)
    buf.WriteString(strconv.FormatInt(m.Sequence, 10))
    buf.WriteString(",\n")
    
    // Field 4: timestamp
    buf.WriteString(`  "timestamp": `)
    buf.WriteString(strconv.FormatInt(m.Timestamp, 10))
    buf.WriteString(",\n")
    
    // Field 5: hash
    buf.WriteString(`  "hash": "` + m.Hash + `",\n`)
    
    // Field 6: content
    buf.WriteString(`  "content": `)
    contentBytes, err := json.Marshal(m.Content)
    if err != nil {
        return nil, err
    }
    buf.Write(contentBytes)
    buf.WriteString("\n}")
    
    return buf.Bytes(), nil
}

// Sign computes and sets the signature on the message
func (m *Message) Sign(priv ed25519.PrivateKey) error {
    signingBytes, err := m.marshalForSigning()
    if err != nil {
        return err
    }
    
    sig := ed25519.Sign(priv, signingBytes)
    
    // Signature format: base64 + ".sig.ed25519"
    m.Signature = base64.StdEncoding.EncodeToString(sig) + ".sig.ed25519"
    return nil
}
```

### Marshal With Signature

```go
// marshalWithSignature creates the final JSON including signature
func (m *Message) marshalWithSignature() ([]byte, error) {
    // First marshal without signature
    msgBytes, err := m.marshalForSigning()
    if err != nil {
        return nil, err
    }
    
    // Parse as map to add signature
    var msg map[string]interface{}
    if err := json.Unmarshal(msgBytes, &msg); err != nil {
        return nil, err
    }
    
    msg["signature"] = m.Signature
    
    return json.MarshalIndent(msg, "", "  ")
}
```

---

## Signature Verification

From `internal/ssb/message/legacy/message.go`:

```go
package legacy

import (
    "crypto/ed25519"
    "strings"
    "encoding/base64"
    
    "mr-valinskys_adequate_bridge/internal/ssb/keys"
    "mr-valinskys_adequate_bridge/internal/ssb/refs"
)

// Verify checks the message signature
func (m *Message) Verify() bool {
    // Parse author key
    authorKey, err := keys.UnmarshalKey([]byte(m.Author.String()))
    if err != nil {
        return false
    }
    
    // Decode signature
    sigBytes, ok := decodeSignature(m.Signature)
    if !ok {
        return false
    }
    
    // Get signing bytes (without signature field)
    signingBytes, err := m.marshalForSigning()
    if err != nil {
        return false
    }
    
    // Verify Ed25519 signature
    return ed25519.Verify(
        authorKey.MustEd25519(),
        signingBytes,
        sigBytes,
    )
}

// decodeSignature extracts raw bytes from signature string
// Format: base64sig + ".sig.ed25519"
func decodeSignature(sigStr string) ([]byte, bool) {
    const suffix = ".sig.ed25519"
    if !strings.HasSuffix(sigStr, suffix) {
        return nil, false
    }
    
    b64 := sigStr[:len(sigStr)-len(suffix)]
    return base64.StdEncoding.DecodeString(b64)
}
```

---

## Feed Manager for EBT

From `internal/ssb/sbot/feed_manager_adapter.go`:

```go
package sbot

import (
    "encoding/json"
    "fmt"
    
    "github.com/pkg/errors"
    "go.cryptoscope.co/margaret"
    
    "mr-valinskys_adequate_bridge/internal/ssb/refs"
    "mr-valinskys_adequate_bridge/internal/ssb/message/legacy"
)

// FeedManagerAdapter adapts the SSB log for EBT replication
type FeedManagerAdapter struct {
    log margaret.Log
    key refs.FeedRef
}

// NewFeedManagerAdapter creates an adapter for a feed
func NewFeedManagerAdapter(log margaret.Log, key refs.FeedRef) *FeedManagerAdapter {
    return &FeedManagerAdapter{
        log: log,
        key: key,
    }
}

// GetMessage returns a classic-format message for EBT replication
func (fma *FeedManagerAdapter) GetMessage(seq int64) (map[string]interface{}, error) {
    // Get message from log
    msgVal, err := fma.log.Get(seq)
    if err != nil {
        return nil, errors.Wrap(err, "failed to get message")
    }
    
    // Extract message metadata from stored value
    msg := msgVal.(legacy.Message)
    
    // Get previous reference if not first message
    var previous interface{}
    if seq > 1 {
        prevMsg, err := fma.log.Get(seq - 1)
        if err == nil {
            previous = prevMsg.(legacy.Message).Key()
        }
    }
    
    // Get signature for this sequence
    sig := fma.getSignature(seq)
    
    // Format content - extract actual content from message
    content, err := fma.getContent(msg)
    if err != nil {
        return nil, errors.Wrap(err, "failed to get content")
    }
    
    // Classic format for EBT - fields MUST be in specific order
    // This order is required for Tildefriends compatibility
    msgData := map[string]interface{}{
        "previous":  previous,
        "author":    fma.key,
        "sequence":  seq,
        "timestamp": msg.Timestamp,
        "hash":      "sha256",
        "content":   content,
        "signature": sig,
    }
    
    return msgData, nil
}

func (fma *FeedManagerAdapter) getSignature(seq int64) string {
    // Retrieve stored signature for this sequence
    // Implementation depends on how signatures are stored
    return "" // Placeholder
}
```

### Feed Registration

```go
// ListFeeds returns all feeds that should be replicated via EBT
type FeedReplicator struct {
    feeds map[string]*FeedManagerAdapter
}

func (fr *FeedReplicator) ListFeeds() []FeedSeqPair {
    var result []FeedSeqPair
    for ref, adapter := range fr.feeds {
        seq := adapter.CurrentSequence()
        result = append(result, FeedSeqPair{
            FeedRef: ref,
            Sequence: seq,
        })
    }
    return result
}

// CurrentSequence returns the latest sequence for a feed
func (fma *FeedManagerAdapter) CurrentSequence() int64 {
    seq, err := fma.log.Seq()
    if err != nil {
        return -1
    }
    return seq + 1 // Next sequence to publish
}
```

---

## MUXRPC Handler

From `internal/ssb/muxrpc/muxrpc.go`:

```go
package muxrpc

// Header flags
const (
    FlagStream   = 1 << 5  // Part of a stream
    FlagEnd      = 1 << 6  // End of stream
    FlagErr      = 1 << 7  // Error indicator
)

// Body types
const (
    TypeBinary = 0
    TypeJSON   = 1
)

// Packet represents an RPC message
type Packet struct {
    Flag    uint8
    Req     int32
    Body    []byte
}

// Request represents an incoming RPC request
type Request struct {
    Method  Method
    Type    uint8
    Req     int32
    Stream  bool
    End     bool
    Err     bool
    Body    []byte
}

// HandlePacket processes an incoming packet
func (s *Server) HandlePacket(pkt Packet) error {
    if pkt.Flag&FlagEnd != 0 {
        // Stream termination
        return s.handleStreamEnd(pkt)
    }
    
    if pkt.Req < 0 {
        // Response (negative request number)
        return s.handleResponse(pkt)
    }
    
    // Request (positive request number)
    return s.handleRequest(pkt)
}

// Duplex streams for EBT
type duplex struct {
    sink   *PacketSink
    source *PacketSource
    wg     sync.WaitGroup
}
```

### EBT Handler

From `internal/ssb/sbot/ebt_handler.go`:

```go
package sbot

import (
    "context"
    
    "mr-valinskys_adequate_bridge/internal/ssb/muxrpc"
    "mr-valinskys_adequate_bridge/internal/ssb/refs"
)

// EBTHandler implements the ebt.replicate handler
type EBTHandler struct {
    matrix    *StateMatrix
    replicator *FeedReplicator
    self      refs.FeedRef
}

func (s *Sbot) registerEBTHandler() {
    s.handlerMux.Register(
        "ebt",
        "replicate",
        muxrpc.Duplex,
        s.handleEBTReplicate,
    )
}

func (h *EBTHandler) handleEBTReplicate(
    ctx context.Context,
    req *muxrpc.Request,
) error {
    // Parse EBT initialization
    var args struct {
        Version int    `json:"version"`
        Format  string `json:"format"`
    }
    if err := json.Unmarshal(req.Body, &args); err != nil {
        return err
    }
    
    if args.Version != 3 {
        return fmt.Errorf("unsupported EBT version: %d", args.Version)
    }
    if args.Format != "classic" {
        return fmt.Errorf("unsupported format: %s", args.Format)
    }
    
    // Create duplex EBT stream
    duplex := NewEBTDuplex(
        req.Sink(),
        req.Source(),
        h.matrix,
        h.replicator,
        h.self,
    )
    
    // Run the EBT state machine
    return duplex.Run(ctx)
}
```

---

## Box Stream

From `internal/ssb/secretstream/secretstream.go`:

```go
package secretstream

import (
    "crypto_secretbox_easy"
    "golang.org/x/crypto/nacl/box"
    
    "mr-valinskys_adequate_bridge/internal/ssb/secretstream/boxstream"
)

// Session represents an encrypted box stream session
type Session struct {
    r, w       *crypto_secretbox_easy.State
    nonce      [24]byte
    key        [32]byte
}

// Decrypt reads and decrypts a box
func (s *Session) Decrypt(b []byte) ([]byte, error) {
    if len(b) < box overhead {
        return nil, errors.New("box too short")
    }
    
    var nonceCopy [24]byte
    copy(nonceCopy[:], s.nonce[:])
    
    plaintext := make([]byte, len(b)-boxOverhead)
    if !box.OpenEasy(plaintext, b, &nonceCopy, &s.key) {
        return nil, errors.New("decryption failed")
    }
    
    s.incrementNonce()
    return plaintext, nil
}

// Encrypt encrypts data as a box
func (s *Session) Encrypt(plaintext []byte) ([]byte, error) {
    var nonceCopy [24]byte
    copy(nonceCopy[:], s.nonce[:])
    
    ciphertext := make([]byte, len(plaintext)+boxOverhead)
    box.SealEasy(ciphertext, plaintext, &nonceCopy, &s.key)
    
    s.incrementNonce()
    return ciphertext, nil
}
```

### Box Stream Framing

From `internal/ssb/secretstream/boxstream/boxstream.go`:

```go
package boxstream

import (
    "crypto_secretbox_easy"
)

// Header size: 16-byte tag + 2-byte length + 16-byte padding
const HeaderSize = 34

// DecodeHeader extracts length from box stream header
func DecodeHeader(raw []byte) (int, error) {
    if len(raw) < HeaderSize {
        return 0, errors.New("header too short")
    }
    
    // First 16 bytes are the tag
    tag := raw[:16]
    
    // Next 2 bytes are big-endian length
    length := int(raw[16])<<8 | int(raw[17])
    
    return length, nil
}

// Encode creates a box stream header
func Encode(length int) []byte {
    header := make([]byte, HeaderSize)
    
    // Tag is computed from encryption
    // Length in bytes 16-17 (big-endian)
    header[16] = byte(length >> 8)
    header[17] = byte(length)
    
    // Bytes 18-33 are padding (zeroes)
    
    return header
}
```

---

## Secret Handshake

From `internal/ssb/network/network.go`:

```go
package network

import (
    "crypto_ed25519"
    "golang.org/x/crypto/curve25519"
    "golang.org/x/crypto/hkdf"
    "crypto/sha256"
    "hash/hash.go"
)

const (
    NetworkIdentifierSize = 32
    EphemeralKeySize     = 32
    SignatureSize         = 64
)

// ClientHandshake performs the SSB SHS client handshake
func ClientHandshake(
    conn net.Conn,
    clientLongterm *KeyPair,
    serverPubKey []byte,
    networkID []byte,
) (*Session, error) {
    // 1. Generate ephemeral key pair
    ephemeralPub, ephemeralPriv, err := box.GenerateKey(rand.Reader)
    if err != nil {
        return nil, err
    }
    
    // 2. Send ClientHello
    hello := appendHMAC(networkID, ephemeralPub[:])
    if _, err := conn.Write(hello); err != nil {
        return nil, err
    }
    
    // 3. Read ServerHello
    serverHello := make([]byte, 64)
    if _, err := io.ReadFull(conn, serverHello); err != nil {
        return nil, err
    }
    
    // Verify server's HMAC and extract their ephemeral key
    serverHMAC := serverHello[:32]
    serverEphemeral := serverHello[32:]
    if !verifyHMAC(networkID, serverEphemeral, serverHMAC) {
        return nil, errors.New("invalid server hello")
    }
    
    // 4. Derive shared secrets
    sharedAb := scalarMult(ephemeralPriv, serverEphemeral)
    sharedAbServer := scalarMult(ephemeralPriv, serverPubKey)
    
    // 5. Send ClientAuth
    authMsg := buildClientAuth(clientLongterm, networkID, sharedAb, serverPubKey)
    encryptedAuth := secretbox.Seal(nil, authMsg, zeros24[:], deriveKey(sharedAb, sharedAbServer))
    if _, err := conn.Write(encryptedAuth); err != nil {
        return nil, err
    }
    
    // 6. Read ServerAccept
    serverAccept := make([]byte, 80)
    if _, err := io.ReadFull(conn, serverAccept); err != nil {
        return nil, err
    }
    
    // 7. Derive final session key
    sharedAbClient := scalarMult(clientLongterm.Secret, serverEphemeral)
    sessionKey := deriveKey(sharedAb, sharedAbServer, sharedAbClient)
    
    return &Session{
        Conn:      conn,
        CipherKey: sessionKey,
        // ... other fields
    }, nil
}

func deriveKey(secrets ...[]byte) []byte {
    h := hkdf.New(sha256.New, secrets[0], nil, []byte("box_stream_kp"))
    key := make([]byte, 32)
    h.Read(key)
    return key
}
```

---

## Works Cited

### Specifications

| Citation | Source | URL |
|----------|--------|-----|
| SSB Spec | Official SSB Specification | https://spec.scuttlebutt.nz/ |
| Protocol Guide | Scuttlebutt Protocol Guide | https://ssbc.github.io/scuttlebutt-protocol-guide/ |
| SIP-001 | SSB URIs | https://github.com/ssbc/sips/blob/master/001.md |
| SIP-007 | Rooms 2 | https://github.com/ssbc/sips/blob/master/007.md |

### Implementations

| Citation | Source | URL |
|----------|--------|-----|
| EBT Reference | epidemic-broadcast-trees | https://github.com/ssbc/epidemic-broadcast-trees |
| ssb-ebt | ssb-ebt | https://github.com/ssbc/ssb-ebt |
| go-ssb | Go implementation | https://github.com/ssbc/go-ssb |
| Tildefriends | Bridge target | https://github.com/planetary-social/tildefriends |

### Protocol Documentation

| Citation | Description |
|----------|-------------|
| EBT Docs | https://dev.planetary.social/replication/ebt.html |
| RPC Manifest | https://spec.scuttlebutt.nz/rpc/manifest.html |
| EBT Replicate | https://spec.scuttlebutt.nz/rpc/ebt_replicate.html |

### Academic Papers

| Citation | Title | Author |
|----------|-------|--------|
| Plumtree | Epidemic Broadcast Trees | Leitao et al., 2007 |
| SHS Paper | Secret Handshake | https://dominictarr.github.io/secret-handshake-paper/shs.pdf |

---

## See Also

- **[SSB Protocol Fundamentals](ssb-protocol-fundamentals.md)** - Identity, feeds, messages, signing
- **[SSB Replication](ssb-replication.md)** - EBT and createHistoryStream protocols
- **[SSB Rooms](ssb-rooms.md)** - Room2 server protocol and tunnel connections
- **[EBT Replication Debugging](../ebt-replication.md)** - Bridge-specific EBT implementation notes
