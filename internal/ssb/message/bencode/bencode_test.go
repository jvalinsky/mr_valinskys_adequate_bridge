package bencode

import (
	"bytes"
	"math"
	"testing"
)

// TestEncodeString_Empty tests empty string
func TestEncodeString_Empty(t *testing.T) {
	b, err := Encode("")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "0:" {
		t.Errorf("expected '0:', got '%s'", string(b))
	}
}

// TestEncodeString_ASCII tests ASCII string
func TestEncodeString_ASCII(t *testing.T) {
	b, err := Encode("hello")
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "5:hello" {
		t.Errorf("expected '5:hello', got '%s'", string(b))
	}
}

// TestEncodeString_Binary tests binary string
func TestEncodeString_Binary(t *testing.T) {
	b, err := Encode(string([]byte{0x00, 0xFF}))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("2:")) {
		t.Errorf("expected '2:' prefix, got '%s'", string(b[:2]))
	}
}

// TestEncodeBytes tests []byte encoding
func TestEncodeBytes(t *testing.T) {
	b, err := Encode([]byte("abc"))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "3:abc" {
		t.Errorf("expected '3:abc', got '%s'", string(b))
	}
}

// TestEncodeInt_Zero tests zero integer
func TestEncodeInt_Zero(t *testing.T) {
	b, err := Encode(Integer(0))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "i0e" {
		t.Errorf("expected 'i0e', got '%s'", string(b))
	}
}

// TestEncodeInt_Positive tests positive integer
func TestEncodeInt_Positive(t *testing.T) {
	b, err := Encode(Integer(42))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "i42e" {
		t.Errorf("expected 'i42e', got '%s'", string(b))
	}
}

// TestEncodeInt_Negative tests negative integer
func TestEncodeInt_Negative(t *testing.T) {
	b, err := Encode(Integer(-1))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "i-1e" {
		t.Errorf("expected 'i-1e', got '%s'", string(b))
	}
}

// TestEncodeInt_MaxInt64 tests maximum int64
func TestEncodeInt_MaxInt64(t *testing.T) {
	b, err := Encode(Integer(math.MaxInt64))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("i9223372036854775807e")) {
		t.Errorf("wrong encoding for MaxInt64")
	}
}

// TestEncodeInt_MinInt64 tests minimum int64
func TestEncodeInt_MinInt64(t *testing.T) {
	b, err := Encode(Integer(math.MinInt64))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("i-9223372036854775808e")) {
		t.Errorf("wrong encoding for MinInt64")
	}
}

// TestEncodeNativeInt tests native int type
func TestEncodeNativeInt(t *testing.T) {
	b, err := Encode(int(5))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "i5e" {
		t.Errorf("expected 'i5e', got '%s'", string(b))
	}
}

// TestEncodeNativeUint tests native uint type
func TestEncodeNativeUint(t *testing.T) {
	b, err := Encode(uint(5))
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "i5e" {
		t.Errorf("expected 'i5e', got '%s'", string(b))
	}
}

// TestEncodeNil tests nil encoding
func TestEncodeNil(t *testing.T) {
	b, err := Encode(nil)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "0:" {
		t.Errorf("expected '0:', got '%s'", string(b))
	}
}

// TestEncodeList_Empty tests empty list
func TestEncodeList_Empty(t *testing.T) {
	b, err := Encode(List{})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "le" {
		t.Errorf("expected 'le', got '%s'", string(b))
	}
}

// TestEncodeList_Mixed tests mixed list
func TestEncodeList_Mixed(t *testing.T) {
	b, err := Encode(List{"foo", Integer(3)})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "l3:fooi3ee" {
		t.Errorf("expected 'l3:fooi3ee', got '%s'", string(b))
	}
}

// TestEncodeDict_Empty tests empty dict
func TestEncodeDict_Empty(t *testing.T) {
	b, err := Encode(Dict{})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if string(b) != "de" {
		t.Errorf("expected 'de', got '%s'", string(b))
	}
}

// TestEncodeDict_Sorted tests dict key sorting
func TestEncodeDict_Sorted(t *testing.T) {
	b, err := Encode(Dict{"z": "last", "a": "first"})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	// Keys should be sorted: "a" before "z"
	if string(b) != "d1:a5:first1:z4:laste" {
		t.Errorf("wrong sort order: %s", string(b))
	}
}

// TestEncodeDict_KeySortOrder tests key sort with mixed lengths
func TestEncodeDict_KeySortOrder(t *testing.T) {
	b, err := Encode(Dict{"ba": "second", "ab": "first"})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	// "ab" should come before "ba"
	if !bytes.Contains(b, []byte("2:ab")) || bytes.Index(b, []byte("2:ab")) < bytes.Index(b, []byte("2:ba")) {
		t.Errorf("wrong key order: %s", string(b))
	}
}

// TestEncodeUnsupportedType tests unsupported type error
func TestEncodeUnsupportedType(t *testing.T) {
	_, err := Encode(struct{}{})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if err != ErrUnsupportedType {
		t.Errorf("wrong error: %v", err)
	}
}

// TestDecodeInteger_Positive tests positive integer decode
func TestDecodeInteger_Positive(t *testing.T) {
	v, err := DecodeBytes([]byte("i99e"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if v != int64(99) {
		t.Errorf("expected 99, got %v", v)
	}
}

// TestDecodeInteger_Negative tests negative integer decode
func TestDecodeInteger_Negative(t *testing.T) {
	v, err := DecodeBytes([]byte("i-5e"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if v != int64(-5) {
		t.Errorf("expected -5, got %v", v)
	}
}

// TestDecodeInteger_Zero tests zero integer decode
func TestDecodeInteger_Zero(t *testing.T) {
	v, err := DecodeBytes([]byte("i0e"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if v != int64(0) {
		t.Errorf("expected 0, got %v", v)
	}
}

// TestDecodeInteger_Invalid tests invalid integer
func TestDecodeInteger_Invalid(t *testing.T) {
	_, err := DecodeBytes([]byte("ixe"))
	if err == nil {
		t.Fatal("expected error for invalid integer")
	}
	if err != ErrInvalidInteger {
		t.Errorf("wrong error: %v", err)
	}
}

// TestDecodeString_Empty tests empty string decode
func TestDecodeString_Empty(t *testing.T) {
	v, err := DecodeBytes([]byte("0:"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty string, got %v", v)
	}
}

// TestDecodeString_Normal tests normal string decode
func TestDecodeString_Normal(t *testing.T) {
	v, err := DecodeBytes([]byte("4:spam"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if v != "spam" {
		t.Errorf("expected 'spam', got %v", v)
	}
}

// TestDecodeString_Truncated tests truncated string
func TestDecodeString_Truncated(t *testing.T) {
	_, err := DecodeBytes([]byte("10:short"))
	if err == nil {
		t.Fatal("expected error for truncated string")
	}
	if err != ErrUnexpectedEnd {
		t.Errorf("wrong error: %v", err)
	}
}

// TestDecodeList_Empty tests empty list decode
func TestDecodeList_Empty(t *testing.T) {
	v, err := DecodeBytes([]byte("le"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	list, ok := v.([]interface{})
	if !ok {
		t.Fatalf("expected list, got %T", v)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d items", len(list))
	}
}

// TestDecodeList_Nested tests nested list decode
func TestDecodeList_Nested(t *testing.T) {
	v, err := DecodeBytes([]byte("lli1eee"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	list, ok := v.([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("expected list with 1 item")
	}
	innerList, ok := list[0].([]interface{})
	if !ok || len(innerList) != 1 {
		t.Fatalf("expected nested list with 1 item")
	}
}

// TestDecodeDict_Basic tests basic dict decode
func TestDecodeDict_Basic(t *testing.T) {
	v, err := DecodeBytes([]byte("d3:fooi1ee"))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	dict, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("expected dict, got %T", v)
	}
	if dict["foo"] != int64(1) {
		t.Errorf("expected foo=1, got %v", dict["foo"])
	}
}

// TestDecodeDict_Unterminated tests unterminated dict
func TestDecodeDict_Unterminated(t *testing.T) {
	_, err := DecodeBytes([]byte("d3:foo"))
	if err == nil {
		t.Fatal("expected error for unterminated dict")
	}
	if err != ErrUnexpectedEnd {
		t.Errorf("wrong error: %v", err)
	}
}

// TestDecodeUnknownByte tests unknown byte
func TestDecodeUnknownByte(t *testing.T) {
	_, err := DecodeBytes([]byte("x"))
	if err == nil {
		t.Fatal("expected error for unknown byte")
	}
	if err != ErrInvalidBencode {
		t.Errorf("wrong error: %v", err)
	}
}

// TestRoundTrip_StringList tests round-trip encoding/decoding
func TestRoundTrip_StringList(t *testing.T) {
	original := List{"a", "b"}
	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodeBytes(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	decodedList, ok := decoded.([]interface{})
	if !ok || len(decodedList) != 2 {
		t.Fatalf("decode failed: expected list with 2 items")
	}
	if decodedList[0] != "a" || decodedList[1] != "b" {
		t.Errorf("wrong content after round-trip")
	}
}

// TestRoundTrip_NestedDict tests nested dict round-trip
func TestRoundTrip_NestedDict(t *testing.T) {
	original := Dict{
		"k": List{Integer(1), "v"},
	}
	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodeBytes(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	decodedDict, ok := decoded.(map[string]interface{})
	if !ok {
		t.Fatalf("expected dict")
	}
	list, ok := decodedDict["k"].([]interface{})
	if !ok || len(list) != 2 {
		t.Errorf("wrong nested list")
	}
}

// TestRoundTrip_BytesDecodeAsString tests bytes decode as string
func TestRoundTrip_BytesDecodeAsString(t *testing.T) {
	original := []byte("hello")
	encoded, _ := Encode(original)

	decoded, _ := DecodeBytes(encoded)
	// Bytes decode as string, not []byte
	if _, ok := decoded.(string); !ok {
		t.Errorf("expected string, got %T", decoded)
	}
}

// TestDictGetString_Found tests GetString with found key
func TestDictGetString_Found(t *testing.T) {
	d := Dict{"k": "v"}
	v, ok := d.GetString("k")
	if !ok || v != "v" {
		t.Errorf("GetString failed: ok=%v, v=%s", ok, v)
	}
}

// TestDictGetString_Missing tests GetString with missing key
func TestDictGetString_Missing(t *testing.T) {
	d := Dict{}
	_, ok := d.GetString("k")
	if ok {
		t.Error("GetString should return false for missing key")
	}
}

// TestDictGetInt_Int64 tests GetInt with int64 value
func TestDictGetInt_Int64(t *testing.T) {
	d := Dict{"n": int64(7)}
	v, ok := d.GetInt("n")
	if !ok || v != 7 {
		t.Errorf("GetInt failed: ok=%v, v=%d", ok, v)
	}
}

// TestDictGetInt_NativeInt tests GetInt with native int
func TestDictGetInt_NativeInt(t *testing.T) {
	d := Dict{"n": int(7)}
	v, ok := d.GetInt("n")
	if !ok || v != 7 {
		t.Errorf("GetInt failed: ok=%v, v=%d", ok, v)
	}
}

// TestDictGetInt_IntegerType tests GetInt with Integer type
func TestDictGetInt_IntegerType(t *testing.T) {
	d := Dict{"n": Integer(7)}
	v, ok := d.GetInt("n")
	if !ok || v != 7 {
		t.Errorf("GetInt failed: ok=%v, v=%d", ok, v)
	}
}

// TestDictGetInt_WrongType tests GetInt with wrong type
func TestDictGetInt_WrongType(t *testing.T) {
	d := Dict{"n": "seven"}
	_, ok := d.GetInt("n")
	if ok {
		t.Error("GetInt should return false for string value")
	}
}

// TestDictGetList tests GetList
func TestDictGetList(t *testing.T) {
	d := Dict{"l": []interface{}{"x"}}
	v, ok := d.GetList("l")
	if !ok || len(v) != 1 {
		t.Errorf("GetList failed")
	}
}

// TestDictGetDict tests GetDict
func TestDictGetDict(t *testing.T) {
	d := Dict{"d": map[string]interface{}{"k": "v"}}
	v, ok := d.GetDict("d")
	if !ok || v["k"] != "v" {
		t.Errorf("GetDict failed")
	}
}

// TestEncodeString_Helper tests EncodeString helper
func TestEncodeString_Helper(t *testing.T) {
	b := EncodeString("hi")
	if string(b) != "2:hi" {
		t.Errorf("expected '2:hi', got '%s'", string(b))
	}
}

// TestEncodeInt64_Helper tests EncodeInt64 helper
func TestEncodeInt64_Helper(t *testing.T) {
	b := EncodeInt64(-3)
	if string(b) != "i-3e" {
		t.Errorf("expected 'i-3e', got '%s'", string(b))
	}
}

// TestMustEncode_Panic tests MustEncode panics on error
func TestMustEncode_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustEncode should panic for unsupported type")
		}
	}()
	MustEncode(struct{}{})
}

// TestMustEncode_Success tests MustEncode succeeds
func TestMustEncode_Success(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MustEncode panicked: %v", r)
		}
	}()
	b := MustEncode("ok")
	if string(b) != "2:ok" {
		t.Errorf("wrong encoding")
	}
}
