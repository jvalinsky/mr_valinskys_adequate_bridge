package backfill

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

var (
	ErrUnsupportedDIDMethod = errors.New("unsupported DID method")
	ErrMissingPDSEndpoint   = errors.New("missing atproto_pds service endpoint")
	ErrInvalidPDSEndpoint   = errors.New("invalid atproto_pds service endpoint")
)

type DIDStatus string

const (
	StatusSuccess         DIDStatus = "success"
	StatusAuthRequired    DIDStatus = "auth_required"
	StatusNotFound        DIDStatus = "not_found"
	StatusMalformedDIDDoc DIDStatus = "malformed_did_doc"
	StatusUnsupportedDID  DIDStatus = "unsupported_did"
	StatusTransportError  DIDStatus = "transport_error"
)

type DIDResult struct {
	DID     string
	PDSHost string
	Status  DIDStatus
	Stats   Stats
	Err     error
}

type HostResolver interface {
	ResolvePDSEndpoint(ctx context.Context, did string) (string, error)
}

type RepoFetcher interface {
	FetchRepo(ctx context.Context, host, did string) ([]byte, error)
}

type FixedHostResolver struct {
	Host string
}

func (r FixedHostResolver) ResolvePDSEndpoint(_ context.Context, _ string) (string, error) {
	return NormalizeServiceEndpoint(r.Host)
}

type DIDPDSResolver struct {
	PLCURL     string
	HTTPClient *http.Client
}

func (r DIDPDSResolver) ResolvePDSEndpoint(ctx context.Context, did string) (string, error) {
	parsed, err := syntax.ParseDID(strings.TrimSpace(did))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsupportedDIDMethod, err)
	}
	if parsed.Method() != "plc" {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedDIDMethod, parsed.Method())
	}

	dir := identity.BaseDirectory{
		PLCURL:     strings.TrimRight(strings.TrimSpace(r.PLCURL), "/"),
		HTTPClient: *configuredHTTPClient(r.HTTPClient),
	}
	doc, err := dir.ResolveDID(ctx, parsed)
	if err != nil {
		return "", err
	}

	rawEndpoint, err := extractPDSEndpoint(doc)
	if err != nil {
		return "", err
	}
	return NormalizeServiceEndpoint(rawEndpoint)
}

type XRPCRepoFetcher struct {
	HTTPClient *http.Client
}

func (f XRPCRepoFetcher) FetchRepo(ctx context.Context, host, did string) ([]byte, error) {
	client := &xrpc.Client{
		Host:   strings.TrimRight(strings.TrimSpace(host), "/"),
		Client: configuredHTTPClient(f.HTTPClient),
	}
	return atproto.SyncGetRepo(ctx, client, did, "")
}

func configuredHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func extractPDSEndpoint(doc *identity.DIDDocument) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("%w: nil DID document", ErrMissingPDSEndpoint)
	}
	for _, svc := range doc.Service {
		parts := strings.SplitN(svc.ID, "#", 2)
		if len(parts) != 2 || parts[1] != "atproto_pds" {
			continue
		}
		if strings.TrimSpace(svc.ServiceEndpoint) == "" {
			return "", fmt.Errorf("%w: empty endpoint", ErrMissingPDSEndpoint)
		}
		return svc.ServiceEndpoint, nil
	}
	return "", fmt.Errorf("%w", ErrMissingPDSEndpoint)
}

func NormalizeServiceEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: empty endpoint", ErrInvalidPDSEndpoint)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPDSEndpoint, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrInvalidPDSEndpoint, u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidPDSEndpoint)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func classifyDIDResult(err error) DIDStatus {
	if err == nil {
		return StatusSuccess
	}
	if errors.Is(err, ErrUnsupportedDIDMethod) {
		return StatusUnsupportedDID
	}
	if errors.Is(err, ErrMissingPDSEndpoint) || errors.Is(err, ErrInvalidPDSEndpoint) || errors.Is(err, identity.ErrDIDResolutionFailed) {
		return StatusMalformedDIDDoc
	}
	if errors.Is(err, identity.ErrDIDNotFound) {
		return StatusNotFound
	}

	var xerr *xrpc.Error
	if errors.As(err, &xerr) {
		switch xerr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return StatusAuthRequired
		case http.StatusNotFound:
			if xerr.Wrapped != nil && strings.Contains(strings.ToLower(xerr.Wrapped.Error()), "failed to decode xrpc error message") {
				return StatusTransportError
			}
			return StatusNotFound
		default:
			return StatusTransportError
		}
	}

	return StatusTransportError
}
