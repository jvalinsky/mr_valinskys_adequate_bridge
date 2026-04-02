package appbsky

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	cid "github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = math.E
var _ = sort.Sort

func (r *RepoStrongRef) MarshalCBOR(w io.Writer) error {
	if r == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 2

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// t.Uri (string)
	if len("uri") > 1000000 {
		return xerrors.Errorf("Value in field \"uri\" was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("uri"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("uri"); err != nil {
		return err
	}
	if len(r.Uri) > 1000000 {
		return xerrors.Errorf("Value in field r.Uri was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(r.Uri))); err != nil {
		return err
	}
	if _, err := cw.WriteString(r.Uri); err != nil {
		return err
	}

	// t.Cid (string)
	if len("cid") > 1000000 {
		return xerrors.Errorf("Value in field \"cid\" was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("cid"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("cid"); err != nil {
		return err
	}
	if len(r.Cid) > 1000000 {
		return xerrors.Errorf("Value in field r.Cid was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(r.Cid))); err != nil {
		return err
	}
	if _, err := cw.WriteString(r.Cid); err != nil {
		return err
	}

	return nil
}

func (r *RepoStrongRef) UnmarshalCBOR(rawReader io.Reader) error {
	cr := cbg.NewCborReader(rawReader)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return xerrors.Errorf("expected map, got major type %d", maj)
	}

	if extra > cbg.MaxLength {
		return xerrors.Errorf("struct is too large (%d bytes)", extra)
	}

	nameBuf := make([]byte, 1000000)
	for i := uint64(0); i < extra; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}
		if !ok {
			nameBuf = append([]byte{}, nameBuf[:nameLen]...)
		}
		fieldName := string(nameBuf[:nameLen])
		switch fieldName {
		case "uri":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			r.Uri = val
		case "cid":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			r.Cid = val
		default:
			return xerrors.Errorf("unknown field %s in RepoStrongRef", fieldName)
		}
	}

	return nil
}

func (b *LexBlob) MarshalCBOR(w io.Writer) error {
	if b == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// t.Ref (LexLink as string via legacy blob format)
	if len("ref") > 1000000 {
		return xerrors.Errorf("Value in field \"ref\" was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("ref"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("ref"); err != nil {
		return err
	}
	refStr := lexutil.LexLink(b.Ref).String()
	if len(refStr) > 1000000 {
		return xerrors.Errorf("Value in field b.Ref was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(refStr))); err != nil {
		return err
	}
	if _, err := cw.WriteString(refStr); err != nil {
		return err
	}

	// t.MimeType (string)
	if len("mimeType") > 1000000 {
		return xerrors.Errorf("Value in field \"mimeType\" was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("mimeType"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("mimeType"); err != nil {
		return err
	}
	if len(b.MimeType) > 1000000 {
		return xerrors.Errorf("Value in field b.MimeType was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(b.MimeType))); err != nil {
		return err
	}
	if _, err := cw.WriteString(b.MimeType); err != nil {
		return err
	}

	// t.Size (int64)
	if len("size") > 1000000 {
		return xerrors.Errorf("Value in field \"size\" was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("size"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("size"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(b.Size)); err != nil {
		return err
	}

	return nil
}

func (b *LexBlob) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return xerrors.Errorf("expected map, got major type %d", maj)
	}

	if extra > cbg.MaxLength {
		return xerrors.Errorf("struct is too large (%d bytes)", extra)
	}

	nameBuf := make([]byte, 1000000)
	for i := uint64(0); i < extra; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}
		if !ok {
			nameBuf = append([]byte{}, nameBuf[:nameLen]...)
		}
		fieldName := string(nameBuf[:nameLen])
		switch fieldName {
		case "ref":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			parsed, err := cid.Decode(val)
			if err != nil {
				return err
			}
			b.Ref = lexutil.LexLink(parsed)
		case "mimeType":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			b.MimeType = val
		case "size":
			maj, val, err := cr.ReadHeader()
			if err != nil {
				return err
			}
			if maj == cbg.MajUnsignedInt {
				b.Size = int64(val)
			} else if maj == cbg.MajNegativeInt {
				b.Size = int64(-1) - int64(val)
			} else {
				return xerrors.Errorf("expected number for size, got major type %d", maj)
			}
		default:
			return xerrors.Errorf("unknown field %s in LexBlob", fieldName)
		}
	}

	return nil
}

func init() {
	cbornode.RegisterCborType(FeedPost{})
	cbornode.RegisterCborType(FeedPost_ReplyRef{})
	cbornode.RegisterCborType(FeedPost_Embed{})
	cbornode.RegisterCborType(FeedLike{})
	cbornode.RegisterCborType(FeedRepost{})
	cbornode.RegisterCborType(GraphFollow{})
	cbornode.RegisterCborType(GraphBlock{})
	cbornode.RegisterCborType(ActorProfile{})
	cbornode.RegisterCborType(RichtextFacet{})
	cbornode.RegisterCborType(RichtextFacet_ByteSlice{})
	cbornode.RegisterCborType(RichtextFacet_Features_Elem{})
	cbornode.RegisterCborType(RichtextFacet_Link{})
	cbornode.RegisterCborType(RichtextFacet_Mention{})
	cbornode.RegisterCborType(RichtextFacet_Tag{})
	cbornode.RegisterCborType(EmbedDefs_AspectRatio{})
	cbornode.RegisterCborType(EmbedImages{})
	cbornode.RegisterCborType(EmbedImages_Image{})
	cbornode.RegisterCborType(EmbedVideo{})
	cbornode.RegisterCborType(EmbedExternal{})
	cbornode.RegisterCborType(EmbedExternal_External{})
	cbornode.RegisterCborType(EmbedRecord{})
	cbornode.RegisterCborType(EmbedRecordWithMedia{})
	cbornode.RegisterCborType(EmbedRecordWithMedia_Media{})
	cbornode.RegisterCborType(RepoStrongRef{})
	cbornode.RegisterCborType(LexBlob{})
	lexutil.RegisterType("app.bsky.feed.post", &FeedPost{})
	lexutil.RegisterType("app.bsky.feed.like", &FeedLike{})
	lexutil.RegisterType("app.bsky.feed.repost", &FeedRepost{})
	lexutil.RegisterType("app.bsky.graph.follow", &GraphFollow{})
	lexutil.RegisterType("app.bsky.graph.block", &GraphBlock{})
	lexutil.RegisterType("app.bsky.actor.profile", &ActorProfile{})
}

type RepoStrongRef struct {
	Uri string `json:"uri,omitempty" refmt:"uri,omitempty"`
	Cid string `json:"cid,omitempty" refmt:"cid,omitempty"`
}

type LexBlob struct {
	Ref      lexutil.LexLink `json:"ref,omitempty" refmt:"ref,omitempty"`
	MimeType string          `json:"mimeType,omitempty" refmt:"mimeType,omitempty"`
	Size     int64           `json:"size,omitempty" refmt:"size,omitempty"`
}

func (r *RepoStrongRef) UnmarshalJSON(raw []byte) error {
	type alias RepoStrongRef
	var decoded alias
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*r = RepoStrongRef(decoded)
	return nil
}

func (b *LexBlob) UnmarshalJSON(raw []byte) error {
	type alias LexBlob
	var decoded alias
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*b = LexBlob(decoded)
	return nil
}

type FeedPost struct {
	LexiconTypeID string             `json:"$type,omitempty" refmt:"$type,omitempty"`
	CreatedAt     string             `json:"createdAt,omitempty" refmt:"createdAt,omitempty"`
	Embed         *FeedPost_Embed    `json:"embed,omitempty" refmt:"embed,omitempty"`
	Facets        []*RichtextFacet   `json:"facets,omitempty" refmt:"facets,omitempty"`
	Langs         []string           `json:"langs,omitempty" refmt:"langs,omitempty"`
	Reply         *FeedPost_ReplyRef `json:"reply,omitempty" refmt:"reply,omitempty"`
	Tags          []string           `json:"tags,omitempty" refmt:"tags,omitempty"`
	Text          string             `json:"text" refmt:"text"`
}

type FeedPost_ReplyRef struct {
	Root   *RepoStrongRef `json:"root,omitempty" refmt:"root,omitempty"`
	Parent *RepoStrongRef `json:"parent,omitempty" refmt:"parent,omitempty"`
}

type FeedPost_Embed struct {
	EmbedImages          *EmbedImages          `json:"images,omitempty" refmt:"images,omitempty"`
	EmbedVideo           *EmbedVideo           `json:"video,omitempty" refmt:"video,omitempty"`
	EmbedExternal        *EmbedExternal        `json:"external,omitempty" refmt:"external,omitempty"`
	EmbedRecord          *EmbedRecord          `json:"record,omitempty" refmt:"record,omitempty"`
	EmbedRecordWithMedia *EmbedRecordWithMedia `json:"recordWithMedia,omitempty" refmt:"recordWithMedia,omitempty"`
}

type FeedLike struct {
	LexiconTypeID string         `json:"$type,omitempty" refmt:"$type,omitempty"`
	Subject       *RepoStrongRef `json:"subject" refmt:"subject"`
	CreatedAt     string         `json:"createdAt,omitempty" refmt:"createdAt,omitempty"`
}

func (t *FeedLike) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// $type (const string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("$type"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("app.bsky.feed.like"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("app.bsky.feed.like"); err != nil {
		return err
	}

	// subject (RepoStrongRef)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("subject"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("subject"); err != nil {
		return err
	}
	if err := t.Subject.MarshalCBOR(cw); err != nil {
		return err
	}

	// createdAt (string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("createdAt"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("createdAt"); err != nil {
		return err
	}
	if len(t.CreatedAt) > 1000000 {
		return xerrors.Errorf("Value in field t.CreatedAt was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.CreatedAt))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.CreatedAt); err != nil {
		return err
	}

	return nil
}

func (t *FeedLike) UnmarshalCBOR(raw []byte) error {
	return t.UnmarshalCBORReader(strings.NewReader(string(raw)))
}

func (t *FeedLike) UnmarshalCBORReader(rdr io.Reader) error {
	cr := cbg.NewCborReader(rdr)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return xerrors.Errorf("expected map, got major type %d", maj)
	}

	if extra > cbg.MaxLength {
		return xerrors.Errorf("struct is too large (%d bytes)", extra)
	}

	nameBuf := make([]byte, 1000000)
	for i := uint64(0); i < extra; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}
		if !ok {
			nameBuf = append([]byte{}, nameBuf[:nameLen]...)
		}
		fieldName := string(nameBuf[:nameLen])
		switch fieldName {
		case "subject":
			t.Subject = new(RepoStrongRef)
			if err := t.Subject.UnmarshalCBOR(cr); err != nil {
				return err
			}
		case "createdAt":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			t.CreatedAt = val
		default:
			return xerrors.Errorf("unknown field %s in FeedLike", fieldName)
		}
	}

	return nil
}

type FeedRepost struct {
	LexiconTypeID string         `json:"$type,omitempty" refmt:"$type,omitempty"`
	Subject       *RepoStrongRef `json:"subject" refmt:"subject"`
	CreatedAt     string         `json:"createdAt,omitempty" refmt:"createdAt,omitempty"`
}

func (t *FeedRepost) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// $type (const string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("$type"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("app.bsky.feed.repost"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("app.bsky.feed.repost"); err != nil {
		return err
	}

	// subject (RepoStrongRef)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("subject"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("subject"); err != nil {
		return err
	}
	if err := t.Subject.MarshalCBOR(cw); err != nil {
		return err
	}

	// createdAt (string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("createdAt"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("createdAt"); err != nil {
		return err
	}
	if len(t.CreatedAt) > 1000000 {
		return xerrors.Errorf("Value in field t.CreatedAt was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.CreatedAt))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.CreatedAt); err != nil {
		return err
	}

	return nil
}

func (t *FeedRepost) UnmarshalCBOR(raw []byte) error {
	return t.UnmarshalCBORReader(strings.NewReader(string(raw)))
}

func (t *FeedRepost) UnmarshalCBORReader(rdr io.Reader) error {
	cr := cbg.NewCborReader(rdr)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return xerrors.Errorf("expected map, got major type %d", maj)
	}

	if extra > cbg.MaxLength {
		return xerrors.Errorf("struct is too large (%d bytes)", extra)
	}

	nameBuf := make([]byte, 1000000)
	for i := uint64(0); i < extra; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}
		if !ok {
			nameBuf = append([]byte{}, nameBuf[:nameLen]...)
		}
		fieldName := string(nameBuf[:nameLen])
		switch fieldName {
		case "subject":
			t.Subject = new(RepoStrongRef)
			if err := t.Subject.UnmarshalCBOR(cr); err != nil {
				return err
			}
		case "createdAt":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			t.CreatedAt = val
		default:
			return xerrors.Errorf("unknown field %s in FeedRepost", fieldName)
		}
	}

	return nil
}

type GraphFollow struct {
	LexiconTypeID string `json:"$type,omitempty" refmt:"$type,omitempty"`
	Subject       string `json:"subject" refmt:"subject"`
	CreatedAt     string `json:"createdAt,omitempty" refmt:"createdAt,omitempty"`
}

func (t *GraphFollow) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// $type (const string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("$type"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("app.bsky.graph.follow"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("app.bsky.graph.follow"); err != nil {
		return err
	}

	// subject (string - DID)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("subject"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("subject"); err != nil {
		return err
	}
	if len(t.Subject) > 1000000 {
		return xerrors.Errorf("Value in field t.Subject was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Subject))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Subject); err != nil {
		return err
	}

	// createdAt (string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("createdAt"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("createdAt"); err != nil {
		return err
	}
	if len(t.CreatedAt) > 1000000 {
		return xerrors.Errorf("Value in field t.CreatedAt was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.CreatedAt))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.CreatedAt); err != nil {
		return err
	}

	return nil
}

func (t *GraphFollow) UnmarshalCBOR(raw []byte) error {
	return t.UnmarshalCBORReader(strings.NewReader(string(raw)))
}

func (t *GraphFollow) UnmarshalCBORReader(rdr io.Reader) error {
	cr := cbg.NewCborReader(rdr)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return xerrors.Errorf("expected map, got major type %d", maj)
	}

	if extra > cbg.MaxLength {
		return xerrors.Errorf("struct is too large (%d bytes)", extra)
	}

	nameBuf := make([]byte, 1000000)
	for i := uint64(0); i < extra; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}
		if !ok {
			nameBuf = append([]byte{}, nameBuf[:nameLen]...)
		}
		fieldName := string(nameBuf[:nameLen])
		switch fieldName {
		case "subject":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			t.Subject = val
		case "createdAt":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			t.CreatedAt = val
		default:
			return xerrors.Errorf("unknown field %s in GraphFollow", fieldName)
		}
	}

	return nil
}

type GraphBlock struct {
	LexiconTypeID string `json:"$type,omitempty" refmt:"$type,omitempty"`
	Subject       string `json:"subject" refmt:"subject"`
	CreatedAt     string `json:"createdAt,omitempty" refmt:"createdAt,omitempty"`
}

func (t *GraphBlock) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// $type (const string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("$type"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("app.bsky.graph.block"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("app.bsky.graph.block"); err != nil {
		return err
	}

	// subject (string - DID)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("subject"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("subject"); err != nil {
		return err
	}
	if len(t.Subject) > 1000000 {
		return xerrors.Errorf("Value in field t.Subject was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Subject))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Subject); err != nil {
		return err
	}

	// createdAt (string)
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("createdAt"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("createdAt"); err != nil {
		return err
	}
	if len(t.CreatedAt) > 1000000 {
		return xerrors.Errorf("Value in field t.CreatedAt was too long")
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.CreatedAt))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.CreatedAt); err != nil {
		return err
	}

	return nil
}

func (t *GraphBlock) UnmarshalCBOR(raw []byte) error {
	return t.UnmarshalCBORReader(strings.NewReader(string(raw)))
}

func (t *GraphBlock) UnmarshalCBORReader(rdr io.Reader) error {
	cr := cbg.NewCborReader(rdr)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if maj != cbg.MajMap {
		return xerrors.Errorf("expected map, got major type %d", maj)
	}

	if extra > cbg.MaxLength {
		return xerrors.Errorf("struct is too large (%d bytes)", extra)
	}

	nameBuf := make([]byte, 1000000)
	for i := uint64(0); i < extra; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}
		if !ok {
			nameBuf = append([]byte{}, nameBuf[:nameLen]...)
		}
		fieldName := string(nameBuf[:nameLen])
		switch fieldName {
		case "subject":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			t.Subject = val
		case "createdAt":
			val, err := cbg.ReadStringWithMax(cr, 1000000)
			if err != nil {
				return err
			}
			t.CreatedAt = val
		default:
			return xerrors.Errorf("unknown field %s in GraphBlock", fieldName)
		}
	}

	return nil
}

type ActorProfile struct {
	LexiconTypeID string   `json:"$type,omitempty" refmt:"$type,omitempty"`
	DisplayName   *string  `json:"displayName,omitempty" refmt:"displayName,omitempty"`
	Description   *string  `json:"description,omitempty" refmt:"description,omitempty"`
	Avatar        *LexBlob `json:"avatar,omitempty" refmt:"avatar,omitempty"`
	Banner        *LexBlob `json:"banner,omitempty" refmt:"banner,omitempty"`
}

func (t *ActorProfile) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 1
	if t.DisplayName != nil {
		fieldCount++
	}
	if t.Description != nil {
		fieldCount++
	}
	if t.Avatar != nil {
		fieldCount++
	}
	if t.Banner != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("$type"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("app.bsky.actor.profile"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("app.bsky.actor.profile"); err != nil {
		return err
	}

	if t.DisplayName != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("displayName"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("displayName"); err != nil {
			return err
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(*t.DisplayName))); err != nil {
			return err
		}
		if _, err := cw.WriteString(*t.DisplayName); err != nil {
			return err
		}
	}

	if t.Description != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("description"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("description"); err != nil {
			return err
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(*t.Description))); err != nil {
			return err
		}
		if _, err := cw.WriteString(*t.Description); err != nil {
			return err
		}
	}

	if t.Avatar != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("avatar"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("avatar"); err != nil {
			return err
		}
		if err := t.Avatar.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.Banner != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("banner"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("banner"); err != nil {
			return err
		}
		if err := t.Banner.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *ActorProfile) UnmarshalCBOR(r io.Reader) error {
	*t = ActorProfile{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("ActorProfile: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "$type":
			_, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "displayName":
			val, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}
			t.DisplayName = &val
		case "description":
			val, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}
			t.Description = &val
		case "avatar":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Avatar = new(LexBlob)
				if err := t.Avatar.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Avatar pointer: %w", err)
				}
			}
		case "banner":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Banner = new(LexBlob)
				if err := t.Banner.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Banner pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *FeedPost_ReplyRef) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 2

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("root"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("root"); err != nil {
		return err
	}
	if err := t.Root.MarshalCBOR(cw); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("parent"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("parent"); err != nil {
		return err
	}
	if err := t.Parent.MarshalCBOR(cw); err != nil {
		return err
	}

	return nil
}

func (t *FeedPost_ReplyRef) UnmarshalCBOR(r io.Reader) error {
	*t = FeedPost_ReplyRef{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("FeedPost_ReplyRef: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "root":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Root = new(RepoStrongRef)
				if err := t.Root.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Root pointer: %w", err)
				}
			}
		case "parent":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Parent = new(RepoStrongRef)
				if err := t.Parent.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Parent pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *FeedPost) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 2
	if t.CreatedAt != "" {
		fieldCount++
	}
	if t.Embed != nil {
		fieldCount++
	}
	if t.Facets != nil {
		fieldCount++
	}
	if t.Langs != nil {
		fieldCount++
	}
	if t.Reply != nil {
		fieldCount++
	}
	if t.Tags != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("$type"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("app.bsky.feed.post"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("app.bsky.feed.post"); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("text"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("text"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Text))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Text); err != nil {
		return err
	}

	if t.CreatedAt != "" {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("createdAt"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("createdAt"); err != nil {
			return err
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.CreatedAt))); err != nil {
			return err
		}
		if _, err := cw.WriteString(t.CreatedAt); err != nil {
			return err
		}
	}

	if t.Embed != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("embed"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("embed"); err != nil {
			return err
		}
		if err := t.Embed.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.Facets != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("facets"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("facets"); err != nil {
			return err
		}
		if len(t.Facets) > 8192 {
			return xerrors.Errorf("Slice value in field t.Facets was too long")
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Facets))); err != nil {
			return err
		}
		for _, v := range t.Facets {
			if err := v.MarshalCBOR(cw); err != nil {
				return err
			}
		}
	}

	if t.Langs != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("langs"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("langs"); err != nil {
			return err
		}
		if len(t.Langs) > 8192 {
			return xerrors.Errorf("Slice value in field t.Langs was too long")
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Langs))); err != nil {
			return err
		}
		for _, v := range t.Langs {
			if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(v))); err != nil {
				return err
			}
			if _, err := cw.WriteString(v); err != nil {
				return err
			}
		}
	}

	if t.Reply != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("reply"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("reply"); err != nil {
			return err
		}
		if err := t.Reply.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.Tags != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("tags"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("tags"); err != nil {
			return err
		}
		if len(t.Tags) > 8192 {
			return xerrors.Errorf("Slice value in field t.Tags was too long")
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Tags))); err != nil {
			return err
		}
		for _, v := range t.Tags {
			if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(v))); err != nil {
				return err
			}
			if _, err := cw.WriteString(v); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *FeedPost) UnmarshalCBOR(r io.Reader) error {
	*t = FeedPost{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("FeedPost: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "$type":
			_, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "text":
			t.Text, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "createdAt":
			t.CreatedAt, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "embed":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Embed = new(FeedPost_Embed)
				if err := t.Embed.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Embed pointer: %w", err)
				}
			}
		case "facets":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Facets: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Facets = make([]*RichtextFacet, extra)
			}

			for i := 0; i < int(extra); i++ {
				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}
					t.Facets[i] = new(RichtextFacet)
					if err := t.Facets[i].UnmarshalCBOR(cr); err != nil {
						return xerrors.Errorf("unmarshaling t.Facets[i] pointer: %w", err)
					}
				}
			}
		case "langs":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Langs: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Langs = make([]string, extra)
			}

			for i := 0; i < int(extra); i++ {
				t.Langs[i], err = cbg.ReadString(cr)
				if err != nil {
					return err
				}
			}
		case "reply":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Reply = new(FeedPost_ReplyRef)
				if err := t.Reply.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Reply pointer: %w", err)
				}
			}
		case "tags":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Tags: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Tags = make([]string, extra)
			}

			for i := 0; i < int(extra); i++ {
				t.Tags[i], err = cbg.ReadString(cr)
				if err != nil {
					return err
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *FeedPost_Embed) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 0
	if t.EmbedImages != nil {
		fieldCount++
	}
	if t.EmbedVideo != nil {
		fieldCount++
	}
	if t.EmbedExternal != nil {
		fieldCount++
	}
	if t.EmbedRecord != nil {
		fieldCount++
	}
	if t.EmbedRecordWithMedia != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if t.EmbedImages != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("images"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("images"); err != nil {
			return err
		}
		if err := t.EmbedImages.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.EmbedVideo != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("video"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("video"); err != nil {
			return err
		}
		if err := t.EmbedVideo.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.EmbedExternal != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("external"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("external"); err != nil {
			return err
		}
		if err := t.EmbedExternal.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.EmbedRecord != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("record"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("record"); err != nil {
			return err
		}
		if err := t.EmbedRecord.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.EmbedRecordWithMedia != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("recordWithMedia"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("recordWithMedia"); err != nil {
			return err
		}
		if err := t.EmbedRecordWithMedia.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *FeedPost_Embed) UnmarshalCBOR(r io.Reader) error {
	*t = FeedPost_Embed{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("FeedPost_Embed: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "images":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedImages = new(EmbedImages)
				if err := t.EmbedImages.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedImages pointer: %w", err)
				}
			}
		case "video":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedVideo = new(EmbedVideo)
				if err := t.EmbedVideo.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedVideo pointer: %w", err)
				}
			}
		case "external":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedExternal = new(EmbedExternal)
				if err := t.EmbedExternal.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedExternal pointer: %w", err)
				}
			}
		case "record":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedRecord = new(EmbedRecord)
				if err := t.EmbedRecord.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedRecord pointer: %w", err)
				}
			}
		case "recordWithMedia":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedRecordWithMedia = new(EmbedRecordWithMedia)
				if err := t.EmbedRecordWithMedia.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedRecordWithMedia pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedRecordWithMedia) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 0
	if t.Record != nil {
		fieldCount++
	}
	if t.Media != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if t.Record != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("record"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("record"); err != nil {
			return err
		}
		if err := t.Record.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.Media != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("media"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("media"); err != nil {
			return err
		}
		if err := t.Media.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedRecordWithMedia) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedRecordWithMedia{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedRecordWithMedia: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "record":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Record = new(EmbedRecord)
				if err := t.Record.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Record pointer: %w", err)
				}
			}
		case "media":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Media = new(EmbedRecordWithMedia_Media)
				if err := t.Media.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Media pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedRecordWithMedia_Media) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 0
	if t.EmbedImages != nil {
		fieldCount++
	}
	if t.EmbedVideo != nil {
		fieldCount++
	}
	if t.EmbedExternal != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if t.EmbedImages != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("images"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("images"); err != nil {
			return err
		}
		if err := t.EmbedImages.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.EmbedVideo != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("video"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("video"); err != nil {
			return err
		}
		if err := t.EmbedVideo.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.EmbedExternal != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("external"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("external"); err != nil {
			return err
		}
		if err := t.EmbedExternal.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedRecordWithMedia_Media) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedRecordWithMedia_Media{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedRecordWithMedia_Media: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "images":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedImages = new(EmbedImages)
				if err := t.EmbedImages.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedImages pointer: %w", err)
				}
			}
		case "video":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedVideo = new(EmbedVideo)
				if err := t.EmbedVideo.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedVideo pointer: %w", err)
				}
			}
		case "external":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.EmbedExternal = new(EmbedExternal)
				if err := t.EmbedExternal.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.EmbedExternal pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

type RichtextFacet struct {
	Index    *RichtextFacet_ByteSlice       `json:"index,omitempty" refmt:"index,omitempty"`
	Features []*RichtextFacet_Features_Elem `json:"features,omitempty" refmt:"features,omitempty"`
}

type RichtextFacet_ByteSlice struct {
	ByteStart int64 `json:"byteStart" refmt:"byteStart"`
	ByteEnd   int64 `json:"byteEnd" refmt:"byteEnd"`
}

type RichtextFacet_Features_Elem struct {
	RichtextFacet_Link    *RichtextFacet_Link    `json:"link,omitempty" refmt:"link,omitempty"`
	RichtextFacet_Mention *RichtextFacet_Mention `json:"mention,omitempty" refmt:"mention,omitempty"`
	RichtextFacet_Tag     *RichtextFacet_Tag     `json:"tag,omitempty" refmt:"tag,omitempty"`
}

type RichtextFacet_Link struct {
	Uri string `json:"uri" refmt:"uri"`
}

type RichtextFacet_Mention struct {
	Did string `json:"did" refmt:"did"`
}

type RichtextFacet_Tag struct {
	Tag string `json:"tag" refmt:"tag"`
}

type EmbedDefs_AspectRatio struct {
	Width  int64 `json:"width" refmt:"width"`
	Height int64 `json:"height" refmt:"height"`
}

type EmbedImages struct {
	Images []*EmbedImages_Image `json:"images" refmt:"images"`
}

type EmbedImages_Image struct {
	Alt         string                 `json:"alt" refmt:"alt"`
	Image       *LexBlob               `json:"image" refmt:"image"`
	AspectRatio *EmbedDefs_AspectRatio `json:"aspectRatio,omitempty" refmt:"aspectRatio,omitempty"`
}

type EmbedVideo struct {
	Alt         *string                `json:"alt,omitempty" refmt:"alt,omitempty"`
	Video       *LexBlob               `json:"video,omitempty" refmt:"video,omitempty"`
	AspectRatio *EmbedDefs_AspectRatio `json:"aspectRatio,omitempty" refmt:"aspectRatio,omitempty"`
}

type EmbedExternal struct {
	External *EmbedExternal_External `json:"external,omitempty" refmt:"external,omitempty"`
}

type EmbedExternal_External struct {
	Uri         string   `json:"uri,omitempty" refmt:"uri,omitempty"`
	Title       string   `json:"title,omitempty" refmt:"title,omitempty"`
	Description *string  `json:"description,omitempty" refmt:"description,omitempty"`
	Thumb       *LexBlob `json:"thumb,omitempty" refmt:"thumb,omitempty"`
}

type EmbedRecord struct {
	Record *RepoStrongRef `json:"record,omitempty" refmt:"record,omitempty"`
}

type EmbedRecordWithMedia struct {
	Record *EmbedRecord                `json:"record,omitempty" refmt:"record,omitempty"`
	Media  *EmbedRecordWithMedia_Media `json:"media,omitempty" refmt:"media,omitempty"`
}

type EmbedRecordWithMedia_Media struct {
	EmbedImages   *EmbedImages   `json:"images,omitempty" refmt:"images,omitempty"`
	EmbedVideo    *EmbedVideo    `json:"video,omitempty" refmt:"video,omitempty"`
	EmbedExternal *EmbedExternal `json:"external,omitempty" refmt:"external,omitempty"`
}

func (e *FeedPost_Embed) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if part, ok := fields["images"]; ok {
		e.EmbedImages = &EmbedImages{}
		if err := json.Unmarshal(part, e.EmbedImages); err != nil {
			return err
		}
	}
	if part, ok := fields["video"]; ok {
		var video EmbedVideo
		if err := json.Unmarshal(part, &video); err != nil {
			return err
		}
		e.EmbedVideo = &video
	}
	if part, ok := fields["external"]; ok {
		var external EmbedExternal
		if err := json.Unmarshal(part, &external); err != nil {
			return err
		}
		e.EmbedExternal = &external
	}
	if part, ok := fields["record"]; ok {
		var record EmbedRecord
		if err := json.Unmarshal(part, &record); err != nil {
			return err
		}
		e.EmbedRecord = &record
	}
	if part, ok := fields["recordWithMedia"]; ok {
		var recordWithMedia EmbedRecordWithMedia
		if err := json.Unmarshal(part, &recordWithMedia); err != nil {
			return err
		}
		e.EmbedRecordWithMedia = &recordWithMedia
	}
	return nil
}

func (e *EmbedImages) UnmarshalJSON(raw []byte) error {
	if len(raw) > 0 && raw[0] == '[' {
		return json.Unmarshal(raw, &e.Images)
	}
	type alias EmbedImages
	var decoded alias
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*e = EmbedImages(decoded)
	return nil
}

func (e *EmbedExternal) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if _, ok := fields["uri"]; ok {
		var external EmbedExternal_External
		if err := json.Unmarshal(raw, &external); err != nil {
			return err
		}
		e.External = &external
		return nil
	}
	type alias EmbedExternal
	var decoded alias
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*e = EmbedExternal(decoded)
	return nil
}

func (e *EmbedRecord) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if _, ok := fields["uri"]; ok {
		var record RepoStrongRef
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		e.Record = &record
		return nil
	}
	type alias EmbedRecord
	var decoded alias
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*e = EmbedRecord(decoded)
	return nil
}

func (e *EmbedRecordWithMedia_Media) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if part, ok := fields["images"]; ok {
		e.EmbedImages = &EmbedImages{}
		if err := json.Unmarshal(part, e.EmbedImages); err != nil {
			return err
		}
	}
	if part, ok := fields["video"]; ok {
		var video EmbedVideo
		if err := json.Unmarshal(part, &video); err != nil {
			return err
		}
		e.EmbedVideo = &video
	}
	if part, ok := fields["external"]; ok {
		var external EmbedExternal
		if err := json.Unmarshal(part, &external); err != nil {
			return err
		}
		e.EmbedExternal = &external
	}
	return nil
}

func (f *RichtextFacet_Features_Elem) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}

	if part, ok := fields["link"]; ok {
		var link RichtextFacet_Link
		if err := json.Unmarshal(part, &link); err != nil {
			return err
		}
		f.RichtextFacet_Link = &link
	}
	if part, ok := fields["mention"]; ok {
		var mention RichtextFacet_Mention
		if err := json.Unmarshal(part, &mention); err != nil {
			return err
		}
		f.RichtextFacet_Mention = &mention
	}
	if part, ok := fields["tag"]; ok {
		if len(part) > 0 && part[0] == '"' {
			var tagValue string
			if err := json.Unmarshal(part, &tagValue); err != nil {
				return err
			}
			f.RichtextFacet_Tag = &RichtextFacet_Tag{Tag: tagValue}
		} else {
			var tag RichtextFacet_Tag
			if err := json.Unmarshal(part, &tag); err != nil {
				return err
			}
			f.RichtextFacet_Tag = &tag
		}
	}
	if f.RichtextFacet_Link == nil {
		if part, ok := fields["uri"]; ok {
			var uri string
			if err := json.Unmarshal(part, &uri); err != nil {
				return err
			}
			if strings.TrimSpace(uri) != "" {
				f.RichtextFacet_Link = &RichtextFacet_Link{Uri: uri}
			}
		}
	}
	if f.RichtextFacet_Mention == nil {
		if part, ok := fields["did"]; ok {
			var did string
			if err := json.Unmarshal(part, &did); err != nil {
				return err
			}
			if strings.TrimSpace(did) != "" {
				f.RichtextFacet_Mention = &RichtextFacet_Mention{Did: did}
			}
		}
	}
	if f.RichtextFacet_Tag == nil {
		if part, ok := fields["tag"]; ok {
			var tagValue string
			if err := json.Unmarshal(part, &tagValue); err == nil && strings.TrimSpace(tagValue) != "" {
				f.RichtextFacet_Tag = &RichtextFacet_Tag{Tag: tagValue}
			}
		}
	}
	return nil
}

func (t *RichtextFacet) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{162}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("index"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("index"); err != nil {
		return err
	}
	if err := t.Index.MarshalCBOR(cw); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("features"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("features"); err != nil {
		return err
	}

	if len(t.Features) > 8192 {
		return xerrors.Errorf("Slice value in field t.Features was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Features))); err != nil {
		return err
	}
	for _, v := range t.Features {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}
	}
	return nil
}

func (t *RichtextFacet) UnmarshalCBOR(r io.Reader) error {
	*t = RichtextFacet{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("RichtextFacet: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "index":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Index = new(RichtextFacet_ByteSlice)
				if err := t.Index.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Index pointer: %w", err)
				}
			}
		case "features":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Features: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Features = make([]*RichtextFacet_Features_Elem, extra)
			}

			for i := 0; i < int(extra); i++ {
				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}
					t.Features[i] = new(RichtextFacet_Features_Elem)
					if err := t.Features[i].UnmarshalCBOR(cr); err != nil {
						return xerrors.Errorf("unmarshaling t.Features[i] pointer: %w", err)
					}
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *RichtextFacet_ByteSlice) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{162}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("byteEnd"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("byteEnd"); err != nil {
		return err
	}

	if t.ByteEnd >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.ByteEnd)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.ByteEnd-1)); err != nil {
			return err
		}
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("byteStart"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("byteStart"); err != nil {
		return err
	}

	if t.ByteStart >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.ByteStart)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.ByteStart-1)); err != nil {
			return err
		}
	}

	return nil
}

func (t *RichtextFacet_ByteSlice) UnmarshalCBOR(r io.Reader) error {
	*t = RichtextFacet_ByteSlice{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("RichtextFacet_ByteSlice: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "byteEnd":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}
			switch maj {
			case cbg.MajUnsignedInt:
				t.ByteEnd = int64(extra)
			case cbg.MajNegativeInt:
				t.ByteEnd = int64(-1) - int64(extra)
			default:
				return fmt.Errorf("wrong type for t.ByteEnd: %v", maj)
			}
		case "byteStart":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}
			switch maj {
			case cbg.MajUnsignedInt:
				t.ByteStart = int64(extra)
			case cbg.MajNegativeInt:
				t.ByteStart = int64(-1) - int64(extra)
			default:
				return fmt.Errorf("wrong type for t.ByteStart: %v", maj)
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *RichtextFacet_Features_Elem) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if t.RichtextFacet_Mention != nil {
		return t.RichtextFacet_Mention.MarshalCBOR(w)
	}
	if t.RichtextFacet_Link != nil {
		return t.RichtextFacet_Link.MarshalCBOR(w)
	}
	if t.RichtextFacet_Tag != nil {
		return t.RichtextFacet_Tag.MarshalCBOR(w)
	}
	return fmt.Errorf("can not marshal empty union as CBOR")
}

func (t *RichtextFacet_Features_Elem) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)

	maj, _, err := cr.ReadHeader()
	if err != nil {
		return err
	}

	if maj == cbg.MajTag || maj == cbg.MajUnsignedInt || maj == cbg.MajNegativeInt {
		b, err := io.ReadAll(cr)
		if err != nil {
			return err
		}

		typ, err := lexutil.CborTypeExtract(b)
		if err != nil {
			return err
		}

		switch typ {
		case "app.bsky.richtext.facet#mention":
			t.RichtextFacet_Mention = new(RichtextFacet_Mention)
			return json.Unmarshal(b, t.RichtextFacet_Mention)
		case "app.bsky.richtext.facet#link":
			t.RichtextFacet_Link = new(RichtextFacet_Link)
			return json.Unmarshal(b, t.RichtextFacet_Link)
		case "app.bsky.richtext.facet#tag":
			t.RichtextFacet_Tag = new(RichtextFacet_Tag)
			return json.Unmarshal(b, t.RichtextFacet_Tag)
		default:
			return nil
		}
	}

	return nil
}

func (t *EmbedDefs_AspectRatio) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{162}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("width"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("width"); err != nil {
		return err
	}

	if t.Width >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Width)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Width-1)); err != nil {
			return err
		}
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("height"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("height"); err != nil {
		return err
	}

	if t.Height >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Height)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Height-1)); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedDefs_AspectRatio) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedDefs_AspectRatio{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedDefs_AspectRatio: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "width":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}
			switch maj {
			case cbg.MajUnsignedInt:
				t.Width = int64(extra)
			case cbg.MajNegativeInt:
				t.Width = int64(-1) - int64(extra)
			default:
				return fmt.Errorf("wrong type for t.Width: %v", maj)
			}
		case "height":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}
			switch maj {
			case cbg.MajUnsignedInt:
				t.Height = int64(extra)
			case cbg.MajNegativeInt:
				t.Height = int64(-1) - int64(extra)
			default:
				return fmt.Errorf("wrong type for t.Height: %v", maj)
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedImages) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("images"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("images"); err != nil {
		return err
	}

	if len(t.Images) > 8192 {
		return xerrors.Errorf("Slice value in field t.Images was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Images))); err != nil {
		return err
	}
	for _, v := range t.Images {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}
	}
	return nil
}

func (t *EmbedImages) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedImages{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedImages: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "images":
			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > 8192 {
				return fmt.Errorf("t.Images: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Images = make([]*EmbedImages_Image, extra)
			}

			for i := 0; i < int(extra); i++ {
				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}
					t.Images[i] = new(EmbedImages_Image)
					if err := t.Images[i].UnmarshalCBOR(cr); err != nil {
						return xerrors.Errorf("unmarshaling t.Images[i] pointer: %w", err)
					}
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedImages_Image) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 3
	if t.AspectRatio == nil {
		fieldCount--
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("alt"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("alt"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Alt))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Alt); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("image"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("image"); err != nil {
		return err
	}
	if err := t.Image.MarshalCBOR(cw); err != nil {
		return err
	}

	if t.AspectRatio != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("aspectRatio"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("aspectRatio"); err != nil {
			return err
		}
		if err := t.AspectRatio.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedImages_Image) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedImages_Image{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedImages_Image: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "alt":
			t.Alt, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "image":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Image = new(LexBlob)
				if err := t.Image.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Image pointer: %w", err)
				}
			}
		case "aspectRatio":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.AspectRatio = new(EmbedDefs_AspectRatio)
				if err := t.AspectRatio.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.AspectRatio pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedVideo) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 1
	if t.Alt != nil {
		fieldCount++
	}
	if t.Video != nil {
		fieldCount++
	}
	if t.AspectRatio != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if t.Alt != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("alt"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("alt"); err != nil {
			return err
		}
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(*t.Alt))); err != nil {
			return err
		}
		if _, err := cw.WriteString(*t.Alt); err != nil {
			return err
		}
	}

	if t.Video != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("video"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("video"); err != nil {
			return err
		}
		if err := t.Video.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	if t.AspectRatio != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("aspectRatio"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("aspectRatio"); err != nil {
			return err
		}
		if err := t.AspectRatio.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedVideo) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedVideo{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedVideo: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "alt":
			val, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}
			t.Alt = &val
		case "video":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Video = new(LexBlob)
				if err := t.Video.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Video pointer: %w", err)
				}
			}
		case "aspectRatio":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.AspectRatio = new(EmbedDefs_AspectRatio)
				if err := t.AspectRatio.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.AspectRatio pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedExternal) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 1
	if t.External != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if t.External != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("external"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("external"); err != nil {
			return err
		}
		if err := t.External.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedExternal) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedExternal{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedExternal: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "external":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.External = new(EmbedExternal_External)
				if err := t.External.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.External pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedExternal_External) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 3
	if t.Thumb == nil {
		fieldCount--
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("uri"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("uri"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Uri))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Uri); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("title"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("title"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Title))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Title); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("description"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("description"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(*t.Description))); err != nil {
		return err
	}
	if _, err := cw.WriteString(*t.Description); err != nil {
		return err
	}

	if t.Thumb != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("thumb"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("thumb"); err != nil {
			return err
		}
		if err := t.Thumb.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedExternal_External) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedExternal_External{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedExternal_External: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "uri":
			t.Uri, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "title":
			t.Title, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		case "description":
			val, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}
			t.Description = &val
		case "thumb":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Thumb = new(LexBlob)
				if err := t.Thumb.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Thumb pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *EmbedRecord) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	fieldCount := 1
	if t.Record != nil {
		fieldCount++
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	if t.Record != nil {
		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("record"))); err != nil {
			return err
		}
		if _, err := cw.WriteString("record"); err != nil {
			return err
		}
		if err := t.Record.MarshalCBOR(cw); err != nil {
			return err
		}
	}

	return nil
}

func (t *EmbedRecord) UnmarshalCBOR(r io.Reader) error {
	*t = EmbedRecord{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("EmbedRecord: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "record":
			b, err := cr.ReadByte()
			if err != nil {
				return err
			}
			if b != cbg.CborNull[0] {
				if err := cr.UnreadByte(); err != nil {
					return err
				}
				t.Record = new(RepoStrongRef)
				if err := t.Record.UnmarshalCBOR(cr); err != nil {
					return xerrors.Errorf("unmarshaling t.Record pointer: %w", err)
				}
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *RichtextFacet_Link) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("uri"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("uri"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Uri))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Uri); err != nil {
		return err
	}

	return nil
}

func (t *RichtextFacet_Link) UnmarshalCBOR(r io.Reader) error {
	*t = RichtextFacet_Link{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("RichtextFacet_Link: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "uri":
			t.Uri, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *RichtextFacet_Mention) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("did"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("did"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Did))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Did); err != nil {
		return err
	}

	return nil
}

func (t *RichtextFacet_Mention) UnmarshalCBOR(r io.Reader) error {
	*t = RichtextFacet_Mention{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("RichtextFacet_Mention: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "did":
			t.Did, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *RichtextFacet_Tag) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)

	if _, err := cw.Write([]byte{161}); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("tag"))); err != nil {
		return err
	}
	if _, err := cw.WriteString("tag"); err != nil {
		return err
	}
	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Tag))); err != nil {
		return err
	}
	if _, err := cw.WriteString(t.Tag); err != nil {
		return err
	}

	return nil
}

func (t *RichtextFacet_Tag) UnmarshalCBOR(r io.Reader) error {
	*t = RichtextFacet_Tag{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("RichtextFacet_Tag: map struct too large (%d)", extra)
	}

	n := extra

	nameBuf := make([]byte, 8)
	for i := uint64(0); i < n; i++ {
		nameLen, ok, err := cbg.ReadFullStringIntoBuf(cr, nameBuf, 1000000)
		if err != nil {
			return err
		}

		if !ok {
			if err := cbg.ScanForLinks(cr, func(cid.Cid) {}); err != nil {
				return err
			}
			continue
		}

		switch string(nameBuf[:nameLen]) {
		case "tag":
			t.Tag, err = cbg.ReadString(cr)
			if err != nil {
				return err
			}
		default:
			if err := cbg.ScanForLinks(r, func(cid.Cid) {}); err != nil {
				return err
			}
		}
	}

	return nil
}
