package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "atproto-seed",
		Usage: "Seed ATProto messages for e2e testing",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: "http://pds:80",
				Usage: "PDS host URL",
			},
			&cli.StringFlag{
				Name:  "handle",
				Value: "seed.test",
				Usage: "account handle",
			},
			&cli.StringFlag{
				Name:  "password",
				Value: "password",
				Usage: "account password",
			},
			&cli.StringFlag{
				Name:  "target-did",
				Usage: "optional target DID for follow/like records",
			},
			&cli.IntFlag{
				Name:  "post-count",
				Value: 1,
				Usage: "number of posts to publish",
			},
			&cli.StringFlag{
				Name:  "db",
				Value: "bridge.sqlite",
				Usage: "bridge SQLite database path",
			},
			&cli.StringFlag{
				Name:  "bot-seed",
				Value: "e2e-docker-seed",
				Usage: "bot master seed for SSB identity derivation",
			},
		},
		Action: func(c *cli.Context) error {
			host := c.String("host")
			handle := c.String("handle")
			password := c.String("password")
			targetDID := c.String("target-did")
			postCount := c.Int("post-count")
			dbPath := c.String("db")
			botSeed := c.String("bot-seed")

			client := &xrpc.Client{Host: host}
			ctx := context.Background()

			email := handle + "@example.test"
			// Create account (ignore error if exists)
			out, err := atproto.ServerCreateAccount(ctx, client, &atproto.ServerCreateAccount_Input{
				Email:    &email,
				Handle:   handle,
				Password: &password,
			})
			if err != nil {
				log.Printf("create account error (may already exist): %v", err)
			} else {
				log.Printf("created account: %s (DID: %s)", handle, out.Did)
			}

			// Create session
			sess, err := atproto.ServerCreateSession(ctx, client, &atproto.ServerCreateSession_Input{
				Identifier: handle,
				Password:   password,
			})
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			client.Auth = &xrpc.AuthInfo{
				AccessJwt:  sess.AccessJwt,
				RefreshJwt: sess.RefreshJwt,
				Handle:     sess.Handle,
				Did:        sess.Did,
			}
			log.Printf("session created for %s", sess.Did)

			if targetDID == "" {
				database, err := db.Open(dbPath)
				if err != nil {
					return fmt.Errorf("open db: %w", err)
				}
				defer database.Close()

				manager := bots.NewManager([]byte(botSeed), nil, nil, nil)
				feedRef, err := manager.GetFeedID(sess.Did)
				if err != nil {
					return fmt.Errorf("derive ref: %w", err)
				}

				if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
					ATDID:     sess.Did,
					SSBFeedID: feedRef.Ref(),
					Active:    true,
				}); err != nil {
					log.Printf("warning: could not register bridged account (it may already exist): %v", err)
				} else {
					log.Printf("registered bridged account: did=%s feed=%s", sess.Did, feedRef.Ref())
				}
			}

			var postURI string
			var postCID string

			// Publish posts
			for i := 0; i < postCount; i++ {
				text := fmt.Sprintf("e2e atproto seed message %d — %d", i+1, time.Now().UnixNano())
				resp, err := atproto.RepoCreateRecord(ctx, client, &atproto.RepoCreateRecord_Input{
					Collection: "app.bsky.feed.post",
					Repo:       sess.Did,
					Record: &lexutil.LexiconTypeDecoder{
						Val: &appbsky.FeedPost{
							Text:      text,
							CreatedAt: time.Now().UTC().Format(time.RFC3339),
						},
					},
				})
				if err != nil {
					return fmt.Errorf("create post %d: %w", i, err)
				}
				log.Printf("published post %d: %s", i+1, resp.Uri)
				postURI = resp.Uri
				postCID = resp.Cid
			}

			// Publish Like if we have a post and target
			if postURI != "" && targetDID != "" {
				_, err := atproto.RepoCreateRecord(ctx, client, &atproto.RepoCreateRecord_Input{
					Collection: "app.bsky.feed.like",
					Repo:       sess.Did,
					Record: &lexutil.LexiconTypeDecoder{
						Val: &appbsky.FeedLike{
							Subject: &appbsky.RepoStrongRef{
								Uri: postURI,
								Cid: postCID,
							},
							CreatedAt: time.Now().UTC().Format(time.RFC3339),
						},
					},
				})
				if err != nil {
					log.Printf("create like error: %v", err)
				} else {
					log.Printf("published like for %s", postURI)
				}

				_, err = atproto.RepoCreateRecord(ctx, client, &atproto.RepoCreateRecord_Input{
					Collection: "app.bsky.graph.follow",
					Repo:       sess.Did,
					Record: &lexutil.LexiconTypeDecoder{
						Val: &appbsky.GraphFollow{
							Subject:   targetDID,
							CreatedAt: time.Now().UTC().Format(time.RFC3339),
						},
					},
				})
				if err != nil {
					log.Printf("create follow error: %v", err)
				} else {
					log.Printf("published follow for %s", targetDID)
				}
			}

			// Write a seed complete marker for the test runner
			if err := os.WriteFile("/data/atproto-seed-complete", []byte(sess.Did), 0o644); err != nil {
				log.Printf("warning: could not write seed marker: %v", err)
			}

			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
