// Package formats centralizes SSB feed/message format classification for
// replication and debug tooling.
package formats

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type Kind string

const (
	KindFeed    Kind = "feed"
	KindMessage Kind = "message"
)

type SupportStatus string

const (
	StatusSupported   SupportStatus = "supported"
	StatusPartial     SupportStatus = "partial"
	StatusUnsupported SupportStatus = "unsupported"
)

type FeedFormat string

const (
	FeedEd25519      FeedFormat = "ed25519"
	FeedBendyButtV1  FeedFormat = "bendybutt-v1"
	FeedGabbyGroveV1 FeedFormat = "gabbygrove-v1"
	FeedBamboo       FeedFormat = "bamboo"
	FeedIndexedV1    FeedFormat = "indexed-v1"
)

type MessageFormat string

const (
	MessageSHA256       MessageFormat = "sha256"
	MessageBendyButtV1  MessageFormat = "bendybutt-v1"
	MessageGabbyGroveV1 MessageFormat = "gabbygrove-v1"
	MessageBamboo       MessageFormat = "bamboo"
	MessageIndexedV1    MessageFormat = "indexed-v1"
)

type Descriptor struct {
	Name        string
	Kind        Kind
	Status      SupportStatus
	Description string
}

type Registry struct {
	feeds    map[FeedFormat]Descriptor
	messages map[MessageFormat]Descriptor
}

var DefaultRegistry = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{
		feeds: map[FeedFormat]Descriptor{
			FeedEd25519: {
				Name:        string(FeedEd25519),
				Kind:        KindFeed,
				Status:      StatusSupported,
				Description: "classic SSB feed over ed25519 SHS identities",
			},
			FeedBendyButtV1: {
				Name:        string(FeedBendyButtV1),
				Kind:        KindFeed,
				Status:      StatusPartial,
				Description: "metafeed/BendyButt feed; parser and validation exist, replication is gated by raw-byte storage",
			},
			FeedGabbyGroveV1: {
				Name:        string(FeedGabbyGroveV1),
				Kind:        KindFeed,
				Status:      StatusUnsupported,
				Description: "recognized feed ref, replication not implemented",
			},
			FeedBamboo: {
				Name:        string(FeedBamboo),
				Kind:        KindFeed,
				Status:      StatusUnsupported,
				Description: "recognized feed ref, replication not implemented",
			},
			FeedIndexedV1: {
				Name:        string(FeedIndexedV1),
				Kind:        KindFeed,
				Status:      StatusUnsupported,
				Description: "recognized feed ref, replication not implemented",
			},
		},
		messages: map[MessageFormat]Descriptor{
			MessageSHA256: {
				Name:        string(MessageSHA256),
				Kind:        KindMessage,
				Status:      StatusSupported,
				Description: "classic signed JSON message ref",
			},
			MessageBendyButtV1: {
				Name:        string(MessageBendyButtV1),
				Kind:        KindMessage,
				Status:      StatusPartial,
				Description: "BendyButt message parser and validation exist, full history compatibility is gated",
			},
			MessageGabbyGroveV1: {
				Name:        string(MessageGabbyGroveV1),
				Kind:        KindMessage,
				Status:      StatusUnsupported,
				Description: "recognized message ref, replication not implemented",
			},
			MessageBamboo: {
				Name:        string(MessageBamboo),
				Kind:        KindMessage,
				Status:      StatusUnsupported,
				Description: "recognized message ref, replication not implemented",
			},
			MessageIndexedV1: {
				Name:        string(MessageIndexedV1),
				Kind:        KindMessage,
				Status:      StatusUnsupported,
				Description: "recognized message ref, replication not implemented",
			},
		},
	}
}

func (r *Registry) Feed(format FeedFormat) (Descriptor, bool) {
	if r == nil {
		r = DefaultRegistry
	}
	d, ok := r.feeds[format]
	return d, ok
}

func (r *Registry) Message(format MessageFormat) (Descriptor, bool) {
	if r == nil {
		r = DefaultRegistry
	}
	d, ok := r.messages[format]
	return d, ok
}

func (r *Registry) FeedStatus(format FeedFormat) SupportStatus {
	if d, ok := r.Feed(format); ok {
		return d.Status
	}
	return StatusUnsupported
}

func (r *Registry) MessageStatus(format MessageFormat) SupportStatus {
	if d, ok := r.Message(format); ok {
		return d.Status
	}
	return StatusUnsupported
}

func (r *Registry) EnsureFeedSupported(format FeedFormat, method, phase string) error {
	if r == nil {
		r = DefaultRegistry
	}
	if r.FeedStatus(format) == StatusSupported {
		return nil
	}
	return UnsupportedFeed(format, method, phase)
}

func (r *Registry) EnsureMessageSupported(format MessageFormat, method, phase string) error {
	if r == nil {
		r = DefaultRegistry
	}
	if r.MessageStatus(format) == StatusSupported {
		return nil
	}
	return UnsupportedMessage(format, method, phase)
}

func FeedFromRef(ref *refs.FeedRef) FeedFormat {
	if ref == nil {
		return ""
	}
	return FeedFormat(ref.Algo())
}

func MessageFromRef(ref *refs.MessageRef) MessageFormat {
	if ref == nil {
		return ""
	}
	return MessageFormat(ref.Algo())
}

func FeedFromString(feed string) FeedFormat {
	ref, err := refs.ParseFeedRef(feed)
	if err != nil {
		return ""
	}
	return FeedFromRef(ref)
}

func MessageFromString(message string) MessageFormat {
	ref, err := refs.ParseMessageRef(message)
	if err != nil {
		return ""
	}
	return MessageFromRef(ref)
}

func MessageFromFeed(format FeedFormat) MessageFormat {
	switch format {
	case FeedEd25519:
		return MessageSHA256
	case FeedBendyButtV1:
		return MessageBendyButtV1
	case FeedGabbyGroveV1:
		return MessageGabbyGroveV1
	case FeedBamboo:
		return MessageBamboo
	case FeedIndexedV1:
		return MessageIndexedV1
	default:
		return ""
	}
}

func IsClassic(feed FeedFormat, msg MessageFormat) bool {
	return feed == FeedEd25519 && msg == MessageSHA256
}

var ErrUnsupportedFormat = errors.New("unsupported SSB format")

type UnsupportedFormatError struct {
	Kind          Kind
	FeedFormat    FeedFormat
	MessageFormat MessageFormat
	Method        string
	Phase         string
}

func UnsupportedFeed(format FeedFormat, method, phase string) error {
	return &UnsupportedFormatError{
		Kind:       KindFeed,
		FeedFormat: format,
		Method:     method,
		Phase:      phase,
	}
}

func UnsupportedMessage(format MessageFormat, method, phase string) error {
	return &UnsupportedFormatError{
		Kind:          KindMessage,
		MessageFormat: format,
		Method:        method,
		Phase:         phase,
	}
}

func (e *UnsupportedFormatError) Error() string {
	if e == nil {
		return ""
	}
	errKind := "unsupported_format"
	format := ""
	switch e.Kind {
	case KindFeed:
		errKind = "unsupported_feed_format"
		format = string(e.FeedFormat)
	case KindMessage:
		errKind = "unsupported_message_format"
		format = string(e.MessageFormat)
	}
	var fields []string
	fields = append(fields, errKind)
	if strings.TrimSpace(format) != "" {
		fields = append(fields, "format="+format)
	}
	if strings.TrimSpace(e.Method) != "" {
		fields = append(fields, "method="+strings.TrimSpace(e.Method))
	}
	if strings.TrimSpace(e.Phase) != "" {
		fields = append(fields, "phase="+strings.TrimSpace(e.Phase))
	}
	return strings.Join(fields, " ")
}

func (e *UnsupportedFormatError) Unwrap() error {
	return ErrUnsupportedFormat
}

func ErrorKind(err error) string {
	var unsupported *UnsupportedFormatError
	if errors.As(err, &unsupported) {
		switch unsupported.Kind {
		case KindFeed:
			return "unsupported_feed_format"
		case KindMessage:
			return "unsupported_message_format"
		}
		return "unsupported_format"
	}
	return ""
}

func ParseFeedAlgo(value string) (FeedFormat, error) {
	value = strings.TrimSpace(value)
	format := FeedFormat(value)
	if _, ok := DefaultRegistry.Feed(format); ok {
		return format, nil
	}
	return "", fmt.Errorf("ssb formats: unknown feed format %q", value)
}

func ParseMessageAlgo(value string) (MessageFormat, error) {
	value = strings.TrimSpace(value)
	format := MessageFormat(value)
	if _, ok := DefaultRegistry.Message(format); ok {
		return format, nil
	}
	return "", fmt.Errorf("ssb formats: unknown message format %q", value)
}
