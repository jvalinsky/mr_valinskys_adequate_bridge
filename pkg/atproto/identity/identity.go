package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
)

var (
	ErrDIDNotFound           = errors.New("did not found")
	ErrDIDResolutionFailed   = errors.New("did resolution failed")
	ErrHandleResolutionError = errors.New("handle resolution failed")
	ErrHandleMismatch        = errors.New("handle did mismatch")
)

type DIDDocument struct {
	ID          string               `json:"id"`
	AlsoKnownAs []string             `json:"alsoKnownAs,omitempty"`
	Service     []DIDDocumentService `json:"service,omitempty"`
}

type DIDDocumentService struct {
	ID              string `json:"id"`
	Type            string `json:"type,omitempty"`
	ServiceEndpoint string `json:"serviceEndpoint,omitempty"`
}

type DocService = DIDDocumentService

type BaseDirectory struct {
	PLCURL     string
	HTTPClient http.Client
}

type ResolvedIdentity struct {
	DID      syntax.DID
	Handle   syntax.Handle
	Document *DIDDocument
	PDSURL   string
}

func (d BaseDirectory) ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	switch did.Method() {
	case "plc":
		return d.resolveJSON(ctx, strings.TrimRight(strings.TrimSpace(defaultPLCURL(d.PLCURL)), "/")+"/"+did.String())
	case "web":
		return d.resolveJSON(ctx, didWebURL(did.String()))
	default:
		return nil, fmt.Errorf("%w: unsupported did method %q", ErrDIDResolutionFailed, did.Method())
	}
}

func (d BaseDirectory) ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	if did, err := d.resolveHandleHTTPS(ctx, handle); err == nil {
		return did, nil
	}

	txts, err := net.DefaultResolver.LookupTXT(ctx, "_atproto."+handle.String())
	if err != nil {
		return "", fmt.Errorf("handle resolution failed for %s: %w", handle, err)
	}
	for _, txt := range txts {
		if !strings.HasPrefix(txt, "did=") {
			continue
		}
		return syntax.ParseDID(strings.TrimPrefix(txt, "did="))
	}
	return "", fmt.Errorf("%w: no did record for %s", ErrHandleResolutionError, handle)
}

func (d BaseDirectory) ResolveIdentity(ctx context.Context, identifier string) (ResolvedIdentity, error) {
	if strings.HasPrefix(strings.TrimSpace(identifier), "did:") {
		did, err := syntax.ParseDID(identifier)
		if err != nil {
			return ResolvedIdentity{}, err
		}
		doc, err := d.ResolveDID(ctx, did)
		if err != nil {
			return ResolvedIdentity{}, err
		}
		return ResolvedIdentity{
			DID:      did,
			Document: doc,
			PDSURL:   extractPDSEndpoint(doc),
		}, nil
	}

	handle, err := syntax.ParseHandle(identifier)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	did, err := d.ResolveHandle(ctx, handle)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	doc, err := d.ResolveDID(ctx, did)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	if err := VerifyHandleDocument(handle, did, doc); err != nil {
		return ResolvedIdentity{}, err
	}
	return ResolvedIdentity{
		DID:      did,
		Handle:   handle,
		Document: doc,
		PDSURL:   extractPDSEndpoint(doc),
	}, nil
}

func VerifyHandleDocument(handle syntax.Handle, did syntax.DID, doc *DIDDocument) error {
	if doc == nil || strings.TrimSpace(doc.ID) != did.String() {
		return fmt.Errorf("%w: did document id mismatch", ErrHandleMismatch)
	}
	expected := "at://" + handle.String()
	for _, alias := range doc.AlsoKnownAs {
		if strings.EqualFold(strings.TrimSpace(alias), expected) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s not present in alsoKnownAs", ErrHandleMismatch, expected)
}

func extractPDSEndpoint(doc *DIDDocument) string {
	if doc == nil {
		return ""
	}
	for _, svc := range doc.Service {
		if strings.HasSuffix(strings.TrimSpace(svc.ID), "#atproto_pds") && strings.TrimSpace(svc.ServiceEndpoint) != "" {
			return strings.TrimRight(strings.TrimSpace(svc.ServiceEndpoint), "/")
		}
	}
	return ""
}

func (d BaseDirectory) resolveHandleHTTPS(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	url := "https://" + handle.String() + "/.well-known/atproto-did"
	body, status, err := d.read(ctx, url)
	if err != nil {
		return "", fmt.Errorf("handle resolution failed for %s: %w", handle, err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("handle resolution for %s returned status %d", handle, status)
	}
	return syntax.ParseDID(strings.TrimSpace(string(body)))
}

func (d BaseDirectory) resolveJSON(ctx context.Context, url string) (*DIDDocument, error) {
	body, status, err := d.read(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("DID resolution failed: %w", err)
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrDIDNotFound
	default:
		return nil, fmt.Errorf("%w: status=%d", ErrDIDResolutionFailed, status)
	}

	var doc DIDDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode DID document: %w", err)
	}
	return &doc, nil
}

func (d BaseDirectory) read(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func (d BaseDirectory) httpClient() *http.Client {
	if d.HTTPClient.Timeout > 0 || d.HTTPClient.Transport != nil {
		return &d.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func defaultPLCURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "https://plc.directory"
	}
	return raw
}

func didWebURL(did string) string {
	suffix := strings.TrimPrefix(did, "did:web:")
	parts := strings.Split(suffix, ":")
	if len(parts) == 1 {
		return "https://" + parts[0] + "/.well-known/did.json"
	}
	host := parts[0]
	path := strings.Join(parts[1:], "/")
	return "https://" + host + "/" + path + "/did.json"
}
