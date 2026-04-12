package atproto

import (
	"context"
	"testing"
)

// TestRepoGetRecord_Validation tests parameter validation
func TestRepoGetRecord_Validation(t *testing.T) {
	// Mock client that should never be called
	var callCount int
	mockClient := &mockLexClient{
		doFunc: func(ctx context.Context, method string, inputEncoding string, endpoint string, params map[string]any, bodyData any, out any) error {
			callCount++
			return nil
		},
	}

	tests := []struct {
		name       string
		collection string
		repo       string
		rkey       string
		wantErr    bool
		errContain string
	}{
		{
			name:       "valid_did_and_nsid",
			collection: "app.bsky.feed.post",
			repo:       "did:plc:z72i7hdynmk6r22w27h6cj7f",
			rkey:       "3kxb3qd4jf26p",
			wantErr:    false,
		},
		{
			name:       "valid_handle_and_nsid",
			collection: "app.bsky.feed.post",
			repo:       "alice.test",
			rkey:       "3kxb3qd4jf26p",
			wantErr:    false,
		},
		{
			name:       "invalid_repo_not_did_or_handle",
			collection: "app.bsky.feed.post",
			repo:       "not-a-valid-repo",
			rkey:       "3kxb3qd4jf26p",
			wantErr:    true,
			errContain: "repo must be valid DID or handle",
		},
		{
			name:       "invalid_collection_not_nsid",
			collection: "invalid",
			repo:       "did:plc:z72i7hdynmk6r22w27h6cj7f",
			rkey:       "3kxb3qd4jf26p",
			wantErr:    true,
			errContain: "collection must be valid NSID",
		},
		{
			name:       "invalid_rkey_is_dot",
			collection: "app.bsky.feed.post",
			repo:       "did:plc:z72i7hdynmk6r22w27h6cj7f",
			rkey:       ".",
			wantErr:    true,
			errContain: "rkey must be valid record key",
		},
		{
			name:       "invalid_rkey_is_dotdot",
			collection: "app.bsky.feed.post",
			repo:       "did:plc:z72i7hdynmk6r22w27h6cj7f",
			rkey:       "..",
			wantErr:    true,
			errContain: "rkey must be valid record key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount = 0
			_, err := RepoGetRecord(context.Background(), mockClient, "", tt.collection, tt.repo, tt.rkey)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContain != "" && !contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContain)
				}
				// Client should not be called for validation failures
				if callCount > 0 {
					t.Errorf("mock client was called %d times, expected 0 for validation failure", callCount)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Client should be called for valid params
				if callCount != 1 {
					t.Errorf("mock client was called %d times, expected 1", callCount)
				}
			}
		})
	}
}

// mockLexClient implements lexutil.LexClient for testing
type mockLexClient struct {
	doFunc func(ctx context.Context, method string, inputEncoding string, endpoint string, params map[string]any, bodyData any, out any) error
}

func (m *mockLexClient) LexDo(ctx context.Context, method string, inputEncoding string, endpoint string, params map[string]any, bodyData any, out any) error {
	if m.doFunc != nil {
		return m.doFunc(ctx, method, inputEncoding, endpoint, params, bodyData, out)
	}
	return nil
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
