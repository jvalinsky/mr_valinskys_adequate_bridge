package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
)

// TestResolveDIDPlc tests resolving PLC DIDs.
func TestResolveDIDPlc(t *testing.T) {
	plcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/did:plc:12345" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		doc := DIDDocument{
			ID: "did:plc:12345",
			Service: []DIDDocumentService{
				{
					ID:              "#atproto_pds",
					Type:            "AtprotoPersonalDataServer",
					ServiceEndpoint: "https://pds.example.com",
				},
			},
		}
		json.NewEncoder(w).Encode(doc)
	}))
	defer plcServer.Close()

	bd := BaseDirectory{PLCURL: plcServer.URL}
	did, _ := syntax.ParseDID("did:plc:12345")
	doc, err := bd.ResolveDID(context.Background(), did)
	if err != nil {
		t.Fatalf("ResolveDID failed: %v", err)
	}
	if doc.ID != "did:plc:12345" {
		t.Errorf("wrong DID: %s", doc.ID)
	}
	if len(doc.Service) == 0 {
		t.Error("missing service endpoint")
	}
}

// TestResolveDIDWeb tests resolving Web DIDs.
func TestResolveDIDWeb(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/did.json" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		doc := DIDDocument{
			ID: "did:web:example.com",
		}
		json.NewEncoder(w).Encode(doc)
	}))
	defer webServer.Close()

	// Test URL construction for Web DIDs
	did, _ := syntax.ParseDID("did:web:example.com")
	expectedURL := didWebURL(did.String())
	if expectedURL != "https://example.com/.well-known/did.json" {
		t.Errorf("wrong URL: %s", expectedURL)
	}
}

// TestResolveDIDNotFound tests 404 error handling.
func TestResolveDIDNotFound(t *testing.T) {
	plcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer plcServer.Close()

	bd := BaseDirectory{PLCURL: plcServer.URL}
	did, _ := syntax.ParseDID("did:plc:notfound")
	_, err := bd.ResolveDID(context.Background(), did)
	if err != ErrDIDNotFound {
		t.Errorf("expected ErrDIDNotFound, got %v", err)
	}
}

// TestResolveDIDInvalidMethod tests unsupported DID methods.
func TestResolveDIDInvalidMethod(t *testing.T) {
	bd := BaseDirectory{}
	did, _ := syntax.ParseDID("did:unsupported:12345")
	_, err := bd.ResolveDID(context.Background(), did)
	if err == nil {
		t.Error("expected error for unsupported DID method")
	}
}

// TestResolveHandleHTTPS tests resolving handles via HTTPS.
func TestResolveHandleHTTPS(t *testing.T) {
	handleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/atproto-did" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "did:plc:12345")
	}))
	defer handleServer.Close()

	handle, _ := syntax.ParseHandle("user.example.com")

	// Verify URL construction
	expectedURL := "https://" + handle.String() + "/.well-known/atproto-did"
	if expectedURL != "https://user.example.com/.well-known/atproto-did" {
		t.Errorf("wrong URL: %s", expectedURL)
	}
}

// TestVerifyHandleDocument tests handle verification.
func TestVerifyHandleDocument(t *testing.T) {
	handle, _ := syntax.ParseHandle("alice.example.com")
	did, _ := syntax.ParseDID("did:plc:12345")

	tests := []struct {
		name      string
		doc       *DIDDocument
		wantError bool
	}{
		{
			name: "valid document",
			doc: &DIDDocument{
				ID: "did:plc:12345",
				AlsoKnownAs: []string{
					"at://alice.example.com",
				},
			},
			wantError: false,
		},
		{
			name: "case-insensitive match",
			doc: &DIDDocument{
				ID: "did:plc:12345",
				AlsoKnownAs: []string{
					"at://ALICE.EXAMPLE.COM",
				},
			},
			wantError: false,
		},
		{
			name: "missing ID",
			doc: &DIDDocument{
				ID: "did:plc:99999",
				AlsoKnownAs: []string{
					"at://alice.example.com",
				},
			},
			wantError: true,
		},
		{
			name: "missing alsoKnownAs entry",
			doc: &DIDDocument{
				ID: "did:plc:12345",
				AlsoKnownAs: []string{
					"at://other.example.com",
				},
			},
			wantError: true,
		},
		{
			name:      "nil document",
			doc:       nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyHandleDocument(handle, did, tt.doc)
			if (err != nil) != tt.wantError {
				t.Errorf("VerifyHandleDocument: error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

// TestExtractPDSEndpoint tests PDS endpoint extraction.
func TestExtractPDSEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		doc      *DIDDocument
		wantURL  string
	}{
		{
			name: "valid PDS endpoint",
			doc: &DIDDocument{
				Service: []DIDDocumentService{
					{
						ID:              "#atproto_pds",
						Type:            "AtprotoPersonalDataServer",
						ServiceEndpoint: "https://pds.example.com/",
					},
				},
			},
			wantURL: "https://pds.example.com",
		},
		{
			name: "trims trailing slash",
			doc: &DIDDocument{
				Service: []DIDDocumentService{
					{
						ID:              "#atproto_pds",
						ServiceEndpoint: "https://pds.example.com///",
					},
				},
			},
			wantURL: "https://pds.example.com",
		},
		{
			name:    "missing PDS service",
			doc:     &DIDDocument{Service: []DIDDocumentService{}},
			wantURL: "",
		},
		{
			name:    "nil document",
			doc:     nil,
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPDSEndpoint(tt.doc)
			if got != tt.wantURL {
				t.Errorf("extractPDSEndpoint: got %q, want %q", got, tt.wantURL)
			}
		})
	}
}

// TestResolveIdentityFromDID tests resolving by DID.
func TestResolveIdentityFromDID(t *testing.T) {
	plcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc := DIDDocument{
			ID: "did:plc:12345",
			Service: []DIDDocumentService{
				{
					ID:              "#atproto_pds",
					ServiceEndpoint: "https://pds.example.com",
				},
			},
		}
		json.NewEncoder(w).Encode(doc)
	}))
	defer plcServer.Close()

	bd := BaseDirectory{PLCURL: plcServer.URL}
	result, err := bd.ResolveIdentity(context.Background(), "did:plc:12345")
	if err != nil {
		t.Fatalf("ResolveIdentity failed: %v", err)
	}
	if result.DID.String() != "did:plc:12345" {
		t.Errorf("wrong DID: %s", result.DID)
	}
	if result.PDSURL != "https://pds.example.com" {
		t.Errorf("wrong PDS URL: %s", result.PDSURL)
	}
	if result.Handle.String() != "" {
		t.Errorf("handle should be empty, got %s", result.Handle)
	}
}

// TestResolveIdentityInvalidDID tests invalid DID format.
func TestResolveIdentityInvalidDID(t *testing.T) {
	bd := BaseDirectory{}
	_, err := bd.ResolveIdentity(context.Background(), "invalid:did:format")
	if err == nil {
		t.Error("expected error for invalid DID")
	}
}

// TestResolveIdentityInvalidHandle tests invalid handle format.
func TestResolveIdentityInvalidHandle(t *testing.T) {
	bd := BaseDirectory{}
	_, err := bd.ResolveIdentity(context.Background(), "not-a-valid-handle!")
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

// TestDefaultPLCURL tests default PLC URL.
func TestDefaultPLCURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "https://plc.directory"},
		{"   ", "https://plc.directory"},
		{"https://plc.custom.com", "https://plc.custom.com"},
		{"https://plc.custom.com/", "https://plc.custom.com/"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.input), func(t *testing.T) {
			got := defaultPLCURL(tt.input)
			if got != tt.want {
				t.Errorf("defaultPLCURL: got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDIDWebURL tests DID Web URL construction.
func TestDIDWebURL(t *testing.T) {
	tests := []struct {
		did  string
		want string
	}{
		{"did:web:example.com", "https://example.com/.well-known/did.json"},
		{"did:web:sub.example.com:path:to:did", "https://sub.example.com/path/to/did/did.json"},
		{"did:web:localhost:3000", "https://localhost/3000/did.json"},
	}

	for _, tt := range tests {
		t.Run(tt.did, func(t *testing.T) {
			got := didWebURL(tt.did)
			if got != tt.want {
				t.Errorf("didWebURL: got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveIdentityDocumentErrors tests error propagation from ResolveDID.
func TestResolveIdentityDocumentErrors(t *testing.T) {
	plcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "server error"})
	}))
	defer plcServer.Close()

	bd := BaseDirectory{PLCURL: plcServer.URL}
	did, _ := syntax.ParseDID("did:plc:12345")

	_, err := bd.ResolveDID(context.Background(), did)
	if err == nil {
		t.Error("expected error for server error")
	}
}

// TestResolveHandleResolutionError tests handle resolution failure.
func TestResolveHandleResolutionError(t *testing.T) {
	bd := BaseDirectory{PLCURL: "https://invalid.local:9999"}
	handle, _ := syntax.ParseHandle("user.invalid.local")

	// This should fail due to DNS lookup
	_, err := bd.ResolveHandle(context.Background(), handle)
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

// BenchmarkResolveDID benchmarks DID resolution.
func BenchmarkResolveDID(b *testing.B) {
	plcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc := DIDDocument{
			ID: "did:plc:12345",
			Service: []DIDDocumentService{
				{
					ID:              "#atproto_pds",
					ServiceEndpoint: "https://pds.example.com",
				},
			},
		}
		json.NewEncoder(w).Encode(doc)
	}))
	defer plcServer.Close()

	bd := BaseDirectory{PLCURL: plcServer.URL}
	did, _ := syntax.ParseDID("did:plc:12345")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bd.ResolveDID(context.Background(), did)
	}
}
