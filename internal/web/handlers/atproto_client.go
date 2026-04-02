package handlers

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

// PDSClientInterface defines the interactions with a PDS.
type PDSClientInterface interface {
	UploadBlob(ctx context.Context, identifier string, reader io.Reader, mime string) (*appbsky.LexBlob, error)
	CreatePost(ctx context.Context, identifier string, text string, imageBlob *appbsky.LexBlob) (string, error)
}

// PDSClient handles XRPC interactions with the PDS.
type PDSClient struct {
	Host     string
	Password string
	Insecure bool
}

var _ PDSClientInterface = (*PDSClient)(nil)

func (c *PDSClient) createSession(ctx context.Context, identifier string) (*xrpc.Client, error) {
	client := &xrpc.Client{Host: c.Host}
	if c.Insecure {
		client.Client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
	sess, err := atproto.ServerCreateSession(ctx, client, &atproto.ServerCreateSession_Input{
		Identifier: identifier,
		Password:   c.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("pds session for %s: %w", identifier, err)
	}

	client.Auth = &xrpc.AuthInfo{
		AccessJwt:  sess.AccessJwt,
		RefreshJwt: sess.RefreshJwt,
		Handle:     sess.Handle,
		Did:        sess.Did,
	}
	return client, nil
}

func (c *PDSClient) UploadBlob(ctx context.Context, identifier string, reader io.Reader, mime string) (*appbsky.LexBlob, error) {
	log.Printf("unit=pds event=upload_blob_start identifier=%q mime=%q", identifier, mime)
	client, err := c.createSession(ctx, identifier)
	if err != nil {
		return nil, err
	}

	resp, err := atproto.RepoUploadBlob(ctx, client, reader)
	if err != nil {
		log.Printf("unit=pds event=upload_blob_error identifier=%q error=%q", identifier, err)
		return nil, fmt.Errorf("upload blob: %w", err)
	}

	log.Printf("unit=pds event=upload_blob_success identifier=%q cid=%q", identifier, resp.Blob.Ref)
	return &appbsky.LexBlob{
		Ref:      resp.Blob.Ref,
		MimeType: resp.Blob.MimeType,
		Size:     resp.Blob.Size,
	}, nil
}

func (c *PDSClient) CreatePost(ctx context.Context, identifier string, text string, imageBlob *appbsky.LexBlob) (string, error) {
	client, err := c.createSession(ctx, identifier)
	if err != nil {
		return "", err
	}

	record := &appbsky.FeedPost{
		Text:      text,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if imageBlob != nil {
		record.Embed = &appbsky.FeedPost_Embed{
			EmbedImages: &appbsky.EmbedImages{
				Images: []*appbsky.EmbedImages_Image{
					{
						Image: imageBlob,
						Alt:   "Uploaded via Bridge Admin UI",
					},
				},
			},
		}
	}

	resp, err := atproto.RepoCreateRecord(ctx, client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       client.Auth.Did,
		Record:     &lexutil.LexiconTypeDecoder{Val: record},
	})
	if err != nil {
		return "", fmt.Errorf("create record: %w", err)
	}

	return resp.Uri, nil
}
