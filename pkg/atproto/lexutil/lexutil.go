package lexutil

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func CborTypeExtract(b []byte) (string, error) {
	cr := cbg.NewCborReader(bytes.NewReader(b))

	maj, _, err := cr.ReadHeader()
	if err != nil {
		return "", err
	}

	if maj == cbg.MajTag || maj == cbg.MajUnsignedInt || maj == cbg.MajNegativeInt {
		typ, _, err := CborTypeExtractReader(bytes.NewReader(b))
		return typ, err
	}

	if maj != cbg.MajMap {
		return "", nil
	}

	nameBuf := make([]byte, 8)
	for {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		if string(nameBuf[:nameLen]) == "$type" {
			typ, err := cbg.ReadString(cr)
			return typ, err
		}
		if _, err := io.ReadAll(cr); err != nil {
			return "", err
		}
		break
	}

	return "", nil
}

func CborTypeExtractReader(r io.Reader) (string, []byte, error) {
	buf := new(bytes.Buffer)
	io.TeeReader(r, buf)

	typ, err := CborTypeExtract(buf.Bytes())
	return typ, buf.Bytes(), err
}

const (
	Query     = "query"
	Procedure = "procedure"
)

type LexClient interface {
	LexDo(ctx context.Context, method string, inputEncoding string, endpoint string, params map[string]any, bodyData any, out any) error
}

type LexLink cid.Cid

type jsonLink struct {
	Link string `json:"$link"`
}

func (ll LexLink) String() string {
	return cid.Cid(ll).String()
}

func (ll LexLink) Defined() bool {
	return cid.Cid(ll).Defined()
}

func (ll LexLink) MarshalJSON() ([]byte, error) {
	if !ll.Defined() {
		return nil, fmt.Errorf("cannot marshal undefined cid link")
	}
	return json.Marshal(jsonLink{Link: ll.String()})
}

func (ll *LexLink) UnmarshalJSON(raw []byte) error {
	var jl jsonLink
	if err := json.Unmarshal(raw, &jl); err != nil {
		return fmt.Errorf("parse cid link json: %w", err)
	}
	decoded, err := cid.Decode(strings.TrimSpace(jl.Link))
	if err != nil {
		return fmt.Errorf("parse cid link: %w", err)
	}
	*ll = LexLink(decoded)
	return nil
}

type LexBytes []byte

type jsonBytes struct {
	Bytes string `json:"$bytes"`
}

func (lb LexBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonBytes{Bytes: base64.RawStdEncoding.EncodeToString([]byte(lb))})
}

func (lb *LexBytes) UnmarshalJSON(raw []byte) error {
	var jb jsonBytes
	if err := json.Unmarshal(raw, &jb); err != nil {
		return fmt.Errorf("parse $bytes json: %w", err)
	}
	decoded, err := base64.RawStdEncoding.DecodeString(jb.Bytes)
	if err != nil {
		return fmt.Errorf("parse $bytes payload: %w", err)
	}
	*lb = LexBytes(decoded)
	return nil
}

type LexBlob struct {
	Ref      LexLink `json:"ref" refmt:"ref"`
	MimeType string  `json:"mimeType" refmt:"mimeType"`
	Size     int64   `json:"size" refmt:"size"`
}

type legacyBlob struct {
	Cid      string `json:"cid"`
	MimeType string `json:"mimeType"`
}

type blobSchema struct {
	TypeID   string  `json:"$type,omitempty" refmt:"$type,omitempty"`
	Ref      LexLink `json:"ref" refmt:"ref"`
	MimeType string  `json:"mimeType" refmt:"mimeType"`
	Size     int64   `json:"size" refmt:"size"`
}

func (b LexBlob) MarshalJSON() ([]byte, error) {
	if b.Size < 0 {
		return json.Marshal(legacyBlob{
			Cid:      b.Ref.String(),
			MimeType: b.MimeType,
		})
	}
	return json.Marshal(blobSchema{
		TypeID:   "blob",
		Ref:      b.Ref,
		MimeType: b.MimeType,
		Size:     b.Size,
	})
}

func (b *LexBlob) UnmarshalJSON(raw []byte) error {
	type tagged struct {
		Type string `json:"$type"`
	}
	var hint tagged
	if err := json.Unmarshal(raw, &hint); err != nil {
		return fmt.Errorf("parse blob type: %w", err)
	}
	if hint.Type == "blob" {
		var schema blobSchema
		if err := json.Unmarshal(raw, &schema); err != nil {
			return fmt.Errorf("parse blob schema: %w", err)
		}
		b.Ref = schema.Ref
		b.MimeType = schema.MimeType
		b.Size = schema.Size
		return nil
	}

	var legacy legacyBlob
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return fmt.Errorf("parse legacy blob: %w", err)
	}
	decoded, err := cid.Decode(strings.TrimSpace(legacy.Cid))
	if err != nil {
		return fmt.Errorf("parse legacy blob cid: %w", err)
	}
	b.Ref = LexLink(decoded)
	b.MimeType = legacy.MimeType
	b.Size = -1
	return nil
}

var (
	typeRegistry = map[string]reflect.Type{}
	typeByValue  = map[reflect.Type]string{}
)

func RegisterType(typeID string, prototype any) {
	if strings.TrimSpace(typeID) == "" {
		panic("lexutil.RegisterType: empty type id")
	}
	typ := reflect.TypeOf(prototype)
	if typ == nil {
		panic("lexutil.RegisterType: nil prototype")
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	typeRegistry[typeID] = typ
	typeByValue[typ] = typeID
}

func NewFromType(typeID string) (any, error) {
	typ, ok := typeRegistry[typeID]
	if !ok {
		return nil, fmt.Errorf("unrecognized lexicon type %q", typeID)
	}
	return reflect.New(typ).Interface(), nil
}

type LexiconTypeDecoder struct {
	Val any
}

func (ltd *LexiconTypeDecoder) UnmarshalJSON(raw []byte) error {
	type tagged struct {
		Type string `json:"$type"`
	}
	var hint tagged
	if err := json.Unmarshal(raw, &hint); err != nil {
		return fmt.Errorf("parse record type: %w", err)
	}
	if strings.TrimSpace(hint.Type) == "" {
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return err
		}
		ltd.Val = generic
		return nil
	}

	value, err := NewFromType(hint.Type)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("decode lexicon %s: %w", hint.Type, err)
	}
	ltd.Val = value
	return nil
}

func (ltd *LexiconTypeDecoder) MarshalJSON() ([]byte, error) {
	if ltd == nil || ltd.Val == nil {
		return nil, fmt.Errorf("marshal nil LexiconTypeDecoder")
	}

	value := reflect.ValueOf(ltd.Val)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return json.Marshal(ltd.Val)
	}

	elem := value.Elem()
	typeID := typeByValue[elem.Type()]
	if field := elem.FieldByName("LexiconTypeID"); field.IsValid() && field.CanSet() && field.Kind() == reflect.String && field.String() == "" && typeID != "" {
		field.SetString(typeID)
	}

	return json.Marshal(ltd.Val)
}

func CborDecodeValue(raw []byte) (any, error) {
	var decoded any
	if err := cbornode.DecodeInto(raw, &decoded); err != nil {
		return nil, err
	}
	return normalizeCBORValue(decoded), nil
}

func normalizeCBORValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = normalizeCBORValue(child)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[fmt.Sprint(key)] = normalizeCBORValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, normalizeCBORValue(child))
		}
		return out
	case cid.Cid:
		return map[string]any{"$link": typed.String()}
	case []byte:
		return map[string]any{"$bytes": base64.RawStdEncoding.EncodeToString(typed)}
	default:
		return typed
	}
}
