package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPDSClientUploadBlob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "com.atproto.server.createSession") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"accessJwt": "fake_access",
				"refreshJwt": "fake_refresh",
				"handle": "alice",
				"did": "did:plc:alice"
			}`))
			return
		}

		if strings.Contains(r.URL.Path, "com.atproto.repo.uploadBlob") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"blob": {
					"$type": "blob",
					"ref": {"$link": "bafkreie56pwhrs36n6u4reid75oifvea2rce3n763vof5qetb7zaj2q7de"},
					"mimeType": "image/png",
					"size": 100
				}
			}`))
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := &PDSClient{
		Host:     server.URL,
		Password: "password",
	}

	blob, err := client.UploadBlob(context.Background(), "alice", strings.NewReader("fake image"), "image/png")
	if err != nil {
		t.Fatalf("UploadBlob failed: %v", err)
	}

	if blob.Ref.String() != "bafkreie56pwhrs36n6u4reid75oifvea2rce3n763vof5qetb7zaj2q7de" {
		t.Fatalf("expected ref bafkreie56pwhrs36n6u4reid75oifvea2rce3n763vof5qetb7zaj2q7de, got %s", blob.Ref.String())
	}
}

func TestPDSClientCreatePost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "com.atproto.server.createSession") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"accessJwt": "fake_access",
				"refreshJwt": "fake_refresh",
				"handle": "alice",
				"did": "did:plc:alice"
			}`))
			return
		}

		if strings.Contains(r.URL.Path, "com.atproto.repo.createRecord") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"uri": "at://did:plc:alice/app.bsky.feed.post/post1",
				"cid": "bafyrecent"
			}`))
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := &PDSClient{
		Host:     server.URL,
		Password: "password",
	}

	uri, err := client.CreatePost(context.Background(), "alice", "hello test", nil)
	if err != nil {
		t.Fatalf("CreatePost failed: %v", err)
	}

	if uri != "at://did:plc:alice/app.bsky.feed.post/post1" {
		t.Fatalf("expected uri at://did:plc:alice/app.bsky.feed.post/post1, got %s", uri)
	}
}

func TestPDSClientCreateSessionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad auth", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := &PDSClient{
		Host:     server.URL,
		Password: "wrong",
	}

	_, err := client.UploadBlob(context.Background(), "alice", nil, "image/png")
	if err == nil {
		t.Fatal("expected error from bad session creation")
	}
}
