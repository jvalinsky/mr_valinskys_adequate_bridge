# Tildefriends Signature Verification Fix Plan

**Date:** 2026-03-30
**Priority:** CRITICAL

---

## Summary of Issues Found

Tildefriends (TF) expects messages in a **specific JSON field order** and signs a **specific subset of the message**. Our bridge currently produces signatures that don't match TF's expected format.

---

## Issue 1: JSON Field Ordering (CRITICAL)

### What TF Expects

From `ssb.c` lines 1184-1191, TF reorders fields to this specific order before signing/verifying:

```c
JS_SetPropertyStr(context, reordered, "previous", ...);    // 1
JS_SetPropertyStr(context, reordered, "author", ...);      // 2
JS_SetPropertyStr(context, reordered, "sequence", ...);     // 3
JS_SetPropertyStr(context, reordered, "timestamp", ...);    // 4
JS_SetPropertyStr(context, reordered, "hash", ...);         // 5
JS_SetPropertyStr(context, reordered, "content", ...);      // 6
JS_SetPropertyStr(context, reordered, "signature", ...);   // 7
```

### What Our Bridge Produces

From `internal/ssb/message/legacy/message.go` line 139:

```go
sortStrings(keys)  // Alphabetical sorting!
```

This produces: `author, content, hash, previous, sequence, timestamp`

### Impact

**The signatures DON'T MATCH** because the JSON bytes are different!

---

## Issue 2: What Data is Signed (CRITICAL)

### From `ssb.c` lines 1107-1117:

```c
tf_ssb_calculate_message_id(context, val, out_id, out_id_size);
JSAtom sigatom = JS_NewAtom(context, "signature");
JS_DeleteProperty(context, val, sigatom, 0);  // <-- DELETE signature first!
JS_FreeAtom(context, sigatom);

// ...

JSValue sigval = JS_JSONStringify(context, val, JS_NULL, JS_NewInt32(context, 2));
const char* sigstr = JS_ToCString(context, sigval);
// sigstr is the JSON WITHOUT the signature field!
```

**TF signs the JSON WITHOUT the signature field!** After deleting the signature, it stringifies the remaining fields.

### Our Current Approach

Our `FeedManagerAdapter.GetMessage()` returns the message WITH the signature field included. The signature in the message was created during publishing using `marshalForSigning()` which doesn't include the signature field - this is correct. But the message we send via EBT includes the signature field.

---

## Issue 3: Signature Format

### From `ssb.c` lines 1119-1130:

```c
const char* sigkind = strstr(str, ".sig.ed25519");  // Must end with this!
uint8_t binsig[crypto_sign_BYTES];
r = tf_base64_decode(str, sigkind - str, binsig, sizeof(binsig));  // Decode WITHOUT suffix
if (r != -1)
{
    r = crypto_sign_verify_detached(binsig, (const uint8_t*)sigstr, strlen(sigstr), publickey);
}
```

**TF expects:**
- Signature string must end with `.sig.ed25519`
- Base64 decode the portion BEFORE `.sig.ed25519`
- Use `crypto_sign_verify_detached(signature, message_bytes, message_len, public_key)`

### Our Current Format

Looking at `feed_manager_adapter.go` line 62:
```go
"signature": fmt.Sprintf("%x", msg.Metadata.Sig),
```

This outputs the signature as **hexadecimal**, NOT base64! This is WRONG.

### Expected Format

```go
"signature": base64.StdEncoding.EncodeToString(msg.Metadata.Sig) + ".sig.ed25519"
```

---

## Issue 4: JSON Pretty-Print Format

### From `ssb.c` line 1117:

```c
JSValue sigval = JS_JSONStringify(context, val, JS_NULL, JS_NewInt32(context, 2));
```

TF uses `JSON.stringify` with indent level **2** (two spaces).

### From `ssb.c` line 1970:

```c
JSValue jsonval = JS_JSONStringify(context, root, JS_NULL, JS_NewInt32(context, 2));
```

When TF creates messages, it also uses indent level **2**.

### Our PrettyPrint

Looking at `message.go` line 114:
```go
indent := strings.Repeat("  ", depth)  // Uses 2 spaces - CORRECT!
```

This is correct. But we need to verify the key ordering issue is fixed.

---

## Detailed Fix Plan

### Step 1: Fix JSON Field Ordering in Signing

**File:** `internal/ssb/message/legacy/sign.go`

The `marshalForSigning()` function currently sorts keys alphabetically. We need to change it to use the **specific order** TF expects.

```go
// Current (alphabetical):
{"author":"@...","content":{},"hash":"sha256","previous":null,"sequence":1,"timestamp":123}

// TF expects (specific order):
{"previous":null,"author":"@...","sequence":1,"timestamp":123,"hash":"sha256","content":{}}
```

**Fix:** Replace `json.Marshal(msg)` with manual ordered marshaling:

```go
func (m *Message) marshalForSigning() ([]byte, error) {
    buf := &bytes.Buffer{}
    buf.WriteString("{\n")
    
    // previous
    buf.WriteString(`  "previous": `)
    if m.Previous != nil {
        buf.WriteString(`"` + m.Previous.String() + `"`)
    } else {
        buf.WriteString("null")
    }
    buf.WriteString(",\n")
    
    // author
    buf.WriteString(`  "author": "` + m.Author.String() + `",\n`)
    
    // sequence
    buf.WriteString(`  "sequence": `)
    buf.WriteString(strconv.FormatInt(m.Sequence, 10))
    buf.WriteString(",\n")
    
    // timestamp
    buf.WriteString(`  "timestamp": `)
    buf.WriteString(strconv.FormatInt(m.Timestamp, 10))
    buf.WriteString(",\n")
    
    // hash
    buf.WriteString(`  "hash": "` + m.Hash + `",\n`)
    
    // content
    buf.WriteString(`  "content": `)
    contentBytes, err := json.Marshal(m.Content)
    if err != nil {
        return nil, err
    }
    buf.Write(contentBytes)
    buf.WriteString("\n}")
    
    return buf.Bytes(), nil
}
```

### Step 2: Verify Signature is Stored Correctly

**File:** `internal/ssb/message/legacy/sign.go` line 145

Current code:
```go
Signature: base64.StdEncoding.EncodeToString(sig) + ".sig.ed25519",
```

This looks correct - it uses base64 encoding with `.sig.ed25519` suffix.

### Step 3: Update FeedManagerAdapter

**File:** `internal/ssb/sbot/feed_manager_adapter.go`

The message returned via EBT must have fields in TF's expected order:

```go
// TF expects: previous, author, sequence, timestamp, hash, content, signature
msgData := map[string]interface{}{
    "previous":  previous,      // 1
    "author":    msg.Metadata.Author,    // 2
    "sequence":  msg.Metadata.Sequence, // 3
    "timestamp": msg.Metadata.Timestamp, // 4
    "hash":      "sha256",               // 5
    "content":   content,                // 6
    "signature": base64Sig + ".sig.ed25519", // 7
}
```

**Critical:** Use a struct with explicit field order or ordered map, NOT a map (maps in Go have random iteration order).

### Step 4: Verify the Signed Data

The message ID is calculated from:
```c
tf_ssb_calculate_message_id(context, val, out_id, out_id_size);
```

We need to check what this function does - likely it hashes the JSON without signature.

### Step 5: Test with TF

Enable TF debug logging to see verification results:
```c
if (verify_flags & k_tf_ssb_verify_flag_debug)
{
    tf_printf("verifying author=%s id=%s signature=%s success=%d\n", author, out_id, str, verified);
    tf_printf("signed string:\n%s\n\n", sigstr);
}
```

---

## Files to Modify

| File | Change |
|------|--------|
| `internal/ssb/message/legacy/sign.go` | Fix `marshalForSigning()` to use TF's field order |
| `internal/ssb/sbot/feed_manager_adapter.go` | Return message with fields in TF's order, base64 signature |

---

## Expected Result

After fix:
- JSON fields in order: `previous, author, sequence, timestamp, hash, content, signature`
- Signature in format: `base64sig.sig.ed25519`
- Signature verification succeeds in TF

---

## Verification Steps

1. Run E2E test with TF debug logging enabled
2. Check for `verifying author=... signature=... success=1` in TF logs
3. Verify `messages_from_bot` count increases

---

## Reference: TF Message Creation (for comparison)

From `ssb.c` lines 1959-1992:

```c
JS_SetPropertyStr(context, root, "previous", have_previous ? JS_NewString(context, actual_previous_id) : JS_NULL);
JS_SetPropertyStr(context, root, "author", JS_NewString(context, author));
JS_SetPropertyStr(context, root, "sequence", JS_NewInt32(context, actual_previous_sequence + 1));
JS_SetPropertyStr(context, root, "timestamp", JS_NewInt64(context, now * 1000LL));
JS_SetPropertyStr(context, root, "hash", JS_NewString(context, "sha256"));
JS_SetPropertyStr(context, root, "content", JS_DupValue(context, message));

// Sign WITHOUT signature field
JSValue jsonval = JS_JSONStringify(context, root, JS_NULL, JS_NewInt32(context, 2));
crypto_sign_detached(signature, &siglen, (const uint8_t*)json, len, private_key);

// Add signature
char signature_base64[crypto_sign_BYTES * 2];
tf_base64_encode(signature, ..., signature_base64, ...);
tf_string_set(signature_base64 + length, ..., ".sig.ed25519");
JS_SetPropertyStr(context, root, "signature", JS_NewString(context, signature_base64));
```
