# SQLite Storage Layer Key Collision Bug Investigation

**Status:** CONFIRMED BUG  
**Severity:** High  
**Impact:** Silent message key mismatches in storage layer  
**Affected Code:** 
- `internal/ssb/storage/sqlite/store.go` lines 269, 363
- `internal/ssb/feedlog/feedlog.go` lines 655, 690

## Summary

The SQLite storage layer incorrectly computes message keys as a truncated hex encoding of JSON content, rather than computing proper SSB message references using SHA256 hashing. This creates a fundamental mismatch between how messages are keyed in the storage layer and how they're referenced in tangle membership tables.

## The Bug

### Current Implementation

In both `store.go` (Log.Append) and `feedlog.go` (logAdapter.Append):

```go
key := fmt.Sprintf("%x", data)[:32]
```

Where `data` is the raw JSON-marshaled message. This computes:
1. Hex-encode the JSON bytes → produces variable-length hex string
2. Take first 32 characters → produces 16 bytes of data (since 2 hex chars = 1 byte)
3. Store this as the message "key"

### What Should Happen

Per SSB protocol and the codebase's own `internal/ssb/message/legacy/message.go`:

```go
func SignedMessageRefFromJSON(input []byte) (*refs.MessageRef, error) {
    canonical, err := marshalSignedMessageJSON(raw, true)
    // ... error handling ...
    return NewMessageRef(HashMessage(canonical))
}

func HashMessage(content []byte) []byte {
    v8Encoded := V8Binary(content)  // V8 UTF-16 encoding quirk
    h := sha256.Sum256(v8Encoded)
    return h[:]
}

// In refs.go, message refs are formatted as:
func (m MessageRef) String() string {
    return "%" + base64.StdEncoding.EncodeToString(m.hash[:]) + "." + string(m.algo)
}
```

**Correct process:**
1. Normalize message to canonical JSON form
2. Apply V8Binary encoding (quirky UTF-16 conversion for legacy compatibility)
3. SHA256 hash the V8-encoded bytes → produces 32-byte hash
4. Base64-encode and append algorithm suffix (".sha256") → produces message reference

**Example correct message reference:**
```
%+ofkHa7VpmLgrdhkjtY9SFYoOOp+F7KiEHlG9y4s8eo=.sha256
```

## Why This Is a Bug

### 1. Silent Key Mismatch

The tangle membership table stores **actual message keys**:
```go
// In tangle/store.go
func (s *Store) AddMessage(ctx context.Context, msgKey, tangleName, rootKey string, parentKeys []string) error {
    _, err := s.db.ExecContext(ctx,
        `INSERT OR REPLACE INTO tangle_membership (message_key, tangle_name, root_key, parent_keys, created_at) VALUES (?, ?, ?, ?, ?)`,
        msgKey, tangleName, rootKey, string(parentJSON), time.Now().Unix())
    return err
}
```

But the messages table stores **truncated hex-encoded JSON**:
```go
// In feedlog.go
key = fmt.Sprintf("%x", data)[:32]  // 16 bytes of data
_, err := tx.Exec(
    "INSERT INTO messages (feed_id, seq, key, value_json, created_at) VALUES (?, ?, ?, ?, ?)",
    l.feedID, nextSeq, key, data, now(),
)

// Then used in tangle join:
_ = l.tangles.AddMessage(context.Background(), key, metadata.TangleName, metadata.Root, metadata.Parents)
```

When querying tangles:
```sql
SELECT tm.message_key, tm.tangle_name, tm.root_key, tm.parent_keys, m.value_json
 FROM tangle_membership tm
 LEFT JOIN messages m ON m.key = tm.message_key
```

The `m.key = tm.message_key` join will **fail to match** because:
- `tm.message_key` is a proper SSB reference like `%+ofk...=.sha256` (44 bytes base64)
- `m.key` is a 32-character hex string (16 bytes binary data)

These values are **never equal**.

### 2. Collision Risk

Taking only the first 32 hex characters of JSON creates collision risk. Example:

```
Message A: {"author":"@alice","seq":1}
Hex:       7b22617574686f722...
First 32:  7b22617574686f722240616c6963652

Message B: {"author":"@alice","seq":2}  
Hex:       7b22617574686f722...  (different byte, but first 32 identical)
First 32:  7b22617574686f722240616c6963652
```

While `messages_key_idx` has a UNIQUE constraint that would catch collisions (with a runtime error), the `messages` table does not — allowing duplicate keys to silently coexist.

### 3. Protocol Incompatibility

The SSB protocol defines message references with specific format and content. This key doesn't match the protocol at all:
- Not a proper hash
- Wrong encoding (hex vs base64)
- Wrong format (no algorithm suffix like ".sha256")
- Incompatible with SSB clients, CRDT systems, or peer synchronization

### 4. Truncation Loses Information

Using only 16 bytes instead of 32 bytes of cryptographic output:
- Reduces hash strength from 128-bit to... well, essentially nothing for collision resistance purposes
- Loses information about message content
- Creates false "collisions" for unrelated messages with similar prefixes

## Where Keys Are Used

1. **Storage layer:** `internal/ssb/storage/sqlite/store.go` — used to store and retrieve messages
2. **Feedlog:** `internal/ssb/feedlog/feedlog.go` — wraps storage with metadata
3. **Tangle membership:** Passed to `tangles.AddMessage()` for DAG ordering
4. **Tangle queries:** JOIN condition assumes keys match between tables
5. **Index:** `idx_messages_key` provides lookups by key

## Evidence of the Bug Being Live

1. The `messages_key_idx` table is created but never actually queried anywhere in the codebase
2. Tangle queries will return `NULL` values for `m.value_json` because the JOIN never matches
3. The code doesn't validate that `tm.message_key` matches `m.key`, so the data corruption is silent

## Recommended Fix

Replace the key computation with proper SSB message hashing:

```go
// Pseudo-code; needs proper imports
import (
    "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
    "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// In Append functions:
// 1. Compute proper message reference
msgRef, err := legacy.SignedMessageRefFromJSON(data)
if err != nil {
    return 0, err
}
key := msgRef.String()  // e.g., "%+ofk...=.sha256"

// 2. Use this key consistently everywhere
```

Or, if message content format varies:

```go
// Generic hash function matching SSB spec
func ComputeMessageKey(data []byte) string {
    h := sha256.Sum256(legacy.V8Binary(data))
    return "%" + base64.StdEncoding.EncodeToString(h[:]) + ".sha256"
}
```

## Testing

Add tests to `internal/ssb/storage/sqlite/store_test.go`:

1. **Round-trip test:** Append message → retrieve via key → verify content matches
2. **Tangle join test:** Append message with tangle metadata → query tangle with join → verify message content returned (not NULL)
3. **Key format test:** Verify keys match SSB message reference format (base64, with ".sha256" suffix)
4. **No collisions test:** Append messages with common prefixes → verify all retrievable under their respective keys

## Reference Materials

- SSB Message Format: `internal/ssb/message/legacy/message.go` (HashMessage, SignedMessageRefFromJSON)
- Message Refs: `internal/ssb/refs/refs.go` (String() formatting)
- V8 Binary Encoding: `internal/ssb/message/legacy/message.go` (V8Binary function)
- Tangle Membership: `internal/ssb/message/tangle/store.go` (AddMessage, GetTangleMessages)

## Follow-up Actions

1. [ ] Confirm the bug causes tangle queries to return NULL values
2. [ ] Write failing tests that expose the JOIN issue
3. [ ] Implement proper message hashing in both store.go and feedlog.go
4. [ ] Update schema/migrations if needed
5. [ ] Verify tangle DAG ordering still works correctly with proper keys
6. [ ] Consider data migration for existing databases
