package handlers

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// PDSClientInterface defines the interactions with a PDS.
type PDSClientInterface interface {
	UploadBlob(ctx context.Context, identifier string, reader io.Reader, mime string) (*lexutil.LexBlob, error)
	CreatePost(ctx context.Context, identifier string, text string, imageBlob *lexutil.LexBlob) (string, error)
}

// PDSClient handles XRPC interactions with the PDS.
type PDSClient struct {
	Host     string
	Password string
}

var _ PDSClientInterface = (*PDSClient)(nil)

func (c *PDSClient) createSession(ctx context.Context, identifier string) (*xrpc.Client, error) {
	client := &xrpc.Client{Host: c.Host}
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

func (c *PDSClient) UploadBlob(ctx context.Context, identifier string, reader io.Reader, mime string) (*lexutil.LexBlob, error) {
	client, err := c.createSession(ctx, identifier)
	if err != nil {
		return nil, err
	}

	resp, err := atproto.RepoUploadBlob(ctx, client, reader)
	if err != nil {
		return nil, fmt.Errorf("upload blob: %w", err)
	}

	return resp.Blob, nil
}

func (c *PDSClient) CreatePost(ctx context.Context, identifier string, text string, imageBlob *lexutil.LexBlob) (string, error) {
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
