# SSB Protocol Fundamentals

This document covers the foundational concepts of the Secure Scuttlebutt (SSB) protocol: identities, feeds, messages, and cryptographic signing.

## Table of Contents

1. [Overview](#overview)
2. [Identity & Cryptography](#identity--cryptography)
3. [Feeds (Sigchains)](#feeds-sigchains)
4. [Message Format](#message-format)
5. [Signing Process](#signing-process)
6. [See Also](#see-also)

---

## Overview

Secure Scuttlebutt is a decentralized protocol optimized for social applications. Its key properties:

- **No central servers** - Peers connect directly to each other
- **Offline-first** - Data syncs when peers reconnect
- **Append-only feeds** - Messages form cryptographically-verified chains
- **Strong identity** - Ed25519 key pairs provide cryptographic authenticity

```
┌─────────────────────────────────────────────────────────────────┐
│                    Secure Scuttlebutt Properties                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐       │
│  │  Identity   │     │   Feeds     │     │ Replication │       │
│  │  (Ed25519) │────▶│  (Sigchain) │────▶│   (EBT)    │       │
│  └─────────────┘     └─────────────┘     └─────────────┘       │
│         │                   │                   │                 │
│         ▼                   ▼                   ▼                 │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐       │
│  │  Everyone   │     │ Hash-linked │     │  Efficient  │       │
│  │  can create│     │  messages   │     │ differential│       │
│  └─────────────┘     └─────────────┘     └─────────────┘       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Identity & Cryptography

### Ed25519 Key Pairs

Every SSB user has an Ed25519 key pair:

```
┌─────────────────────────────────────────────────────────────────┐
│                    SSB Identity Generation                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Ed25519 Key Pair                                              │
│   ┌─────────────────┬─────────────────┐                          │
│   │  Secret Key     │  Public Key     │                          │
│   │  (kept private) │  (shared freely)│                          │
│   └────────┬────────┴────────┬────────┘                          │
│            │                  │                                   │
│            ▼                  ▼                                   │
│   ┌────────────────┐  ┌────────────────┐                         │
│   │ Local storage  │  │ @key.ed25519   │                         │
│   │ ~/.ssb/secret  │  │ (Feed ID)      │                         │
│   └────────────────┘  └────────────────┘                         │
│                                                                  │
│   Feed ID Format: @BASE64PUBKEY.ed25519                         │
│   Example: @FCX/tsDLpubCPKKfIrw4gc+SQkHcaD17s7GI6i/ziWY=.ed25519│
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Cryptographic Primitives

| Primitive | Function | Purpose |
|-----------|----------|---------|
| Ed25519 | `nacl_sign_detached` | Message signing |
| Curve25519 | `nacl_scalarmult` | Shared secret derivation |
| NaCl Secret Box | `nacl_secret_box` | Private message encryption |
| HMAC-SHA-512-256 | `hmac` | Handshake integrity |

---

## Feeds (Sigchains)

Each user has exactly one feed - an append-only linked list of messages.

```
┌─────────────────────────────────────────────────────────────────┐
│                    Sigchain (Append-Only Feed)                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌──────┐    ┌──────┐    ┌──────┐    ┌──────┐             │
│   │ Msg 1 │───▶│ Msg 2 │───▶│ Msg 3 │───▶│ Msg 4 │ ...    │
│   │ seq=1 │    │ seq=2 │    │ seq=3 │    │ seq=4 │         │
│   └──────┘    └──────┘    └──────┘    └──────┘             │
│       ▲            │         │         │                       │
│       │            │         │         │                       │
│    (null)     previous    previous    previous                  │
│                   =Msg 1   =Msg 2     =Msg 3                  │
│                                                                  │
│   Each message references previous → forms hash-linked chain      │
│   Sequence numbers start at 1 and increment sequentially          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Sigchain Properties

1. **Append-only** - Messages cannot be modified or deleted
2. **Hash-linked** - Each message references the previous hash
3. **Signed** - Each message is cryptographically signed by its author
4. **Ordered** - Sequence numbers guarantee order within a feed

---

## Message Format

### Classic Format (JSON)

```json
{
  "previous": "%XphMUkWQtomKjXQvFGfsGYpt69sgEY7Y4Vou9cEuJho=.sha256",
  "author": "@FCX/tsDLpubCPKKfIrw4gc+SQkHcaD17s7GI6i/ziWY=.ed25519",
  "sequence": 2,
  "timestamp": 1514517078157,
  "hash": "sha256",
  "content": {
    "type": "post",
    "text": "Second post!"
  },
  "signature": "z7W1ERg9UYZjNfE72ZwEuJF79khG+eOHWFp6iF+KLuSrw8Lqa6IousK4cCn9T5qFa8E14GVek4cAMmMbjqDnAg==.sig.ed25519"
}
```

### Field Descriptions

| Field | Type | Description |
|-------|------|-------------|
| `previous` | string? | Message ID of previous message, or `null` for first |
| `author` | string | Feed ID (public key) of message author |
| `sequence` | number | Position in feed (1-indexed) |
| `timestamp` | number | Milliseconds since Unix epoch |
| `hash` | string | Hash algorithm (`sha256`) |
| `content` | object | Application data (must have `type` field) |
| `signature` | string | Ed25519 signature: `base64+.sig.ed25519` |

### Field Ordering Requirements

```
┌─────────────────────────────────────────────────────────────────┐
│                    Message Field Ordering                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Standard order (swapped=false):                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ 1. previous                                             │    │
│  │ 2. author                                               │    │
│  │ 3. sequence                                             │    │
│  │ 4. timestamp                                            │    │
│  │ 5. hash                                                 │    │
│  │ 6. content                                              │    │
│  │ 7. signature (added last)                                │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  Alternate order (swapped=true, legacy compatibility):          │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ 1. previous                                             │    │
│  │ 2. sequence                                             │    │
│  │ 3. author                                               │    │
│  │ 4. timestamp                                            │    │
│  │ 5. hash                                                 │    │
│  │ 6. content                                              │    │
│  │ 7. signature (added last)                                │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ⚠ Field ordering matters for signature verification!            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Canonical JSON Format

Messages must be serialized using canonical JSON rules:

- Two spaces for indentation
- Dictionary entries on separate lines
- Newlines as `\n` (LF)
- No trailing newline
- One space after colon
- Empty objects as `{}`, empty arrays as `[]`

---

## Signing Process

### Signing Algorithm

```
┌─────────────────────────────────────────────────────────────────┐
│                    Message Signing Process                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Create message object (without signature)                   │
│      │                                                         │
│      ▼                                                         │
│   2. Format as canonical JSON                                   │
│      │  • Two spaces indentation                                │
│      │  • Specific field order (see above)                      │
│      │  • No trailing whitespace                                │
│      ▼                                                         │
│   3. Compute Ed25519 detached signature                          │
│      │                                                         │
│      │  signature = nacl_sign_detached(                          │
│      │    msg: formatted_json_bytes,                            │
│      │    key: authors_longterm_secret_key                      │
│      │  )                                                       │
│      ▼                                                         │
│   4. Base64 encode + append ".sig.ed25519"                    │
│      │                                                         │
│      │  "z7W1ERg9UYZjNfE72ZwEu...==.sig.ed25519"            │
│      ▼                                                         │
│   5. Add signature as final field                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Signature Format

```
┌─────────────────────────────────────────────────────────────────┐
│                    Signature Format                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   structure: base64_signature + ".sig.ed25519"                  │
│                                                                  │
│   example: "Kxr1SqhOvnm8TY784VHE8kZHCD8RdzFl1tBA==.sig.ed25519"│
│                                                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ Kxr1SqhOvnm8TY784VHE8kZHCD8RdzFl1tBA==  │.sig.ed25519 │  │
│   │           Base64-encoded                    │   Suffix    │  │
│   │              Ed25519                        │  (constant) │  │
│   │               signature                     │             │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                  │
│   Base64 variant: Uses + and / characters (NOT URL-safe)       │
│   Padding: Required (= characters at end)                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Signature Verification

To verify a signature:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Signature Verification                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Remove signature field from message                       │
│   2. Format remaining fields as canonical JSON                   │
│      (must use same field order as sender!)                      │
│   3. Extract public key from author field                       │
│   4. Remove ".sig.ed25519" suffix from signature               │
│   5. Base64 decode the signature bytes                          │
│   6. Verify:                                                   │
│                                                                  │
│      nacl_sign_verify_detached(                                  │
│        sig: decoded_signature,                                   │
│        msg: formatted_json_bytes,                               │
│        key: authors_public_key                                   │
│      )                                                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Bridge Implementation Examples

### Message Signing (Go)

From `internal/ssb/message/legacy/sign.go`:

```go
// marshalForSigning creates canonical JSON for signing
func (m *Message) marshalForSigning() ([]byte, error) {
    buf := &bytes.Buffer{}
    buf.WriteString("{\n")
    
    // Fields must be in specific order for TF compatibility
    buf.WriteString(`  "previous": `)
    if m.Previous != nil {
        buf.WriteString(`"` + m.Previous.String() + `"`)
    } else {
        buf.WriteString("null")
    }
    buf.WriteString(",\n")
    
    buf.WriteString(`  "author": "` + m.Author.String() + `",\n`)
    buf.WriteString(`  "sequence": ` + strconv.FormatInt(m.Sequence, 10) + `,\n`)
    buf.WriteString(`  "timestamp": ` + strconv.FormatInt(m.Timestamp, 10) + `,\n`)
    buf.WriteString(`  "hash": "` + m.Hash + `",\n`)
    
    contentBytes, _ := json.Marshal(m.Content)
    buf.WriteString(`  "content": `)
    buf.Write(contentBytes)
    buf.WriteString("\n}")
    
    return buf.Bytes(), nil
}

// Sign computes and adds the signature
func (m *Message) Sign(priv ed25519.PrivateKey) error {
    signingBytes, err := m.marshalForSigning()
    if err != nil {
        return err
    }
    
    sig := ed25519.Sign(priv, signingBytes)
    m.Signature = base64.StdEncoding.EncodeToString(sig) + ".sig.ed25519"
    return nil
}
```

### Signature Verification (Go)

From `internal/ssb/message/legacy/message.go`:

```go
// Verify checks the message signature
func (m *Message) Verify() bool {
    authorKey, err := keys.UnmarshalKey([]byte(m.Author.String()))
    if err != nil {
        return false
    }
    
    sigBytes, ok := decodeSignature(m.Signature)
    if !ok {
        return false
    }
    
    signingBytes, err := m.marshalForSigning()
    if err != nil {
        return false
    }
    
    return ed25519.Verify(authorKey.MustEd25519(), signingBytes, sigBytes)
}

func decodeSignature(sigStr string) ([]byte, bool) {
    suffix := ".sig.ed25519"
    if !strings.HasSuffix(sigStr, suffix) {
        return nil, false
    }
    return base64.StdEncoding.DecodeString(sigStr[:len(sigStr)-len(suffix)])
}
```

---

## See Also

- **[SSB Replication](ssb-replication.md)** - EBT and createHistoryStream protocols
- **[SSB Rooms](ssb-rooms.md)** - Room2 server protocol and tunnel connections
- **[EBT Replication Debugging](../ebt-replication.md)** - Bridge-specific EBT implementation notes
- [SSB Specification](https://spec.scuttlebutt.nz/)
- [Scuttlebutt Protocol Guide](https://ssbc.github.io/scuttlebutt-protocol-guide/)
