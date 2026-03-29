package bencode

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

var (
	ErrInvalidBencode  = errors.New("bencode: invalid bencode data")
	ErrUnexpectedEnd   = errors.New("bencode: unexpected end of data")
	ErrInvalidDictKey  = errors.New("bencode: dict key must be string")
	ErrInvalidInteger  = errors.New("bencode: invalid integer")
	ErrInvalidString   = errors.New("bencode: invalid string")
	ErrInvalidList     = errors.New("bencode: invalid list")
	ErrInvalidDict     = errors.New("bencode: invalid dict")
	ErrUnsupportedType = errors.New("bencode: unsupported type")
)

type Value interface{}

type String string
type Integer int64
type List []interface{}
type Dict map[string]interface{}

func Encode(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeValue(buf *bytes.Buffer, v interface{}) error {
	switch val := v.(type) {
	case int, int8, int16, int32, int64:
		fmt.Fprintf(buf, "i%de", reflect.ValueOf(val).Int())
	case uint, uint8, uint16, uint32, uint64:
		fmt.Fprintf(buf, "i%de", reflect.ValueOf(val).Uint())
	case string:
		fmt.Fprintf(buf, "%d:%s", len(val), val)
	case []byte:
		fmt.Fprintf(buf, "%d:%s", len(val), string(val))
	case []interface{}:
		buf.WriteByte('l')
		for _, elem := range val {
			if err := encodeValue(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case map[string]interface{}:
		buf.WriteByte('d')
		for _, key := range sortedKeys(val) {
			fmt.Fprintf(buf, "%d:%s", len(key), key)
			if err := encodeValue(buf, val[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case String:
		s := string(val)
		fmt.Fprintf(buf, "%d:%s", len(s), s)
	case Integer:
		fmt.Fprintf(buf, "i%de", int64(val))
	case List:
		buf.WriteByte('l')
		for _, elem := range val {
			if err := encodeValue(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case Dict:
		buf.WriteByte('d')
		for _, key := range val.SortedKeys() {
			fmt.Fprintf(buf, "%d:%s", len(key), key)
			if err := encodeValue(buf, val[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case nil:
		buf.WriteString("0:")
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
	return nil
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func Decode(r io.Reader) (interface{}, error) {
	br := bufio.NewReader(r)
	return decodeValue(br)
}

func DecodeBytes(b []byte) (interface{}, error) {
	return Decode(bytes.NewReader(b))
}

func decodeValue(r *bufio.Reader) (interface{}, error) {
	b, err := r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}

	switch {
	case b == 'i':
		return decodeInteger(r)
	case b == 'l':
		return decodeList(r)
	case b == 'd':
		return decodeDict(r)
	case b >= '0' && b <= '9':
		r.UnreadByte()
		return decodeString(r)
	default:
		return nil, fmt.Errorf("%w: unexpected byte %c", ErrInvalidBencode, b)
	}
}

func decodeInteger(r *bufio.Reader) (int64, error) {
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, ErrUnexpectedEnd
		}
		if b == 'e' {
			break
		}
		buf.WriteByte(b)
	}

	n, err := strconv.ParseInt(buf.String(), 10, 64)
	if err != nil {
		return 0, ErrInvalidInteger
	}
	return n, nil
}

func decodeString(r *bufio.Reader) (string, error) {
	var lenBuf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", ErrUnexpectedEnd
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return "", ErrInvalidString
		}
		lenBuf.WriteByte(b)
	}

	length, err := strconv.Atoi(lenBuf.String())
	if err != nil || length < 0 {
		return "", ErrInvalidString
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", ErrUnexpectedEnd
	}

	return string(data), nil
}

func decodeList(r *bufio.Reader) ([]interface{}, error) {
	var list []interface{}
	for {
		peek, err := r.Peek(1)
		if err != nil {
			return nil, ErrUnexpectedEnd
		}

		if peek[0] == 'e' {
			r.ReadByte()
			break
		}

		val, err := decodeValue(r)
		if err != nil {
			return nil, err
		}
		list = append(list, val)
	}
	return list, nil
}

func decodeDict(r *bufio.Reader) (map[string]interface{}, error) {
	dict := make(map[string]interface{})
	for {
		peek, err := r.Peek(1)
		if err != nil {
			return nil, ErrUnexpectedEnd
		}

		if peek[0] == 'e' {
			r.ReadByte()
			break
		}

		key, err := decodeString(r)
		if err != nil {
			return nil, err
		}

		val, err := decodeValue(r)
		if err != nil {
			return nil, err
		}

		dict[key] = val
	}
	return dict, nil
}

func (d Dict) SortedKeys() []string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func (d Dict) Get(key string) (interface{}, bool) {
	v, ok := d[key]
	return v, ok
}

func (d Dict) GetString(key string) (string, bool) {
	v, ok := d[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func (d Dict) GetInt(key string) (int64, bool) {
	v, ok := d[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case Integer:
		return int64(n), true
	default:
		return 0, false
	}
}

func (d Dict) GetList(key string) ([]interface{}, bool) {
	v, ok := d[key]
	if !ok {
		return nil, false
	}
	l, ok := v.([]interface{})
	return l, ok
}

func (d Dict) GetDict(key string) (map[string]interface{}, bool) {
	v, ok := d[key]
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]interface{})
	return m, ok
}

func EncodeString(s string) []byte {
	encoded, _ := Encode(s)
	return encoded
}

func EncodeInt64(i int64) []byte {
	encoded, _ := Encode(Integer(i))
	return encoded
}

func MustEncode(v interface{}) []byte {
	b, err := Encode(v)
	if err != nil {
		panic(err)
	}
	return b
}

func DecodeB64String(b []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(string(b[2:]))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
