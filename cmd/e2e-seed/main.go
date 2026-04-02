// Command e2e-seed publishes test SSB messages via the bridge's SSB runtime.
// It is used by the e2e-tildefriends Docker test to seed the SSB repo with
// known messages before tildefriends connects and replicates.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "e2e-seed",
		Usage: "Seed SSB messages for e2e testing",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "db",
				Value: "bridge.sqlite",
				Usage: "bridge SQLite database path",
			},
			&cli.StringFlag{
				Name:  "repo-path",
				Value: ".ssb-bridge",
				Usage: "SSB repo path",
			},
			&cli.StringFlag{
				Name:    "bot-seed",
				Value:   "e2e-docker-seed",
				EnvVars: []string{"BOT_SEED"},
				Usage:   "master seed for bot key derivation",
			},
			&cli.StringFlag{
				Name:  "did",
				Usage: "AT DID to publish messages for",
			},
			&cli.BoolFlag{
				Name:    "include-blob-post",
				Value:   true,
				EnvVars: []string{"E2E_SEED_INCLUDE_BLOB_POST"},
				Usage:   "publish an additional blob-mention post after base seed messages",
			},
			&cli.StringFlag{
				Name:  "otel-logs-endpoint",
				Usage: "OTLP logs endpoint; empty disables OTLP log export",
			},
			&cli.StringFlag{
				Name:  "otel-logs-protocol",
				Value: "grpc",
				Usage: "OTLP logs protocol: grpc|http",
			},
			&cli.BoolFlag{
				Name:  "otel-logs-insecure",
				Usage: "disable OTLP transport security for log export",
			},
			&cli.StringFlag{
				Name:  "otel-service-name",
				Value: "e2e-seed",
				Usage: "override OTel service.name resource attribute",
			},
			&cli.StringFlag{
				Name:  "local-log-output",
				Value: "text",
				Usage: "local log output mode: text|none",
			},
		},
		Action: func(c *cli.Context) error {
			dbPath := c.String("db")
			repoPath := c.String("repo-path")
			botSeed := c.String("bot-seed")
			did := strings.TrimSpace(c.String("did"))
			includeBlobPost := c.Bool("include-blob-post")

			if did == "" {
				return fmt.Errorf("--did is required")
			}

			logRuntime, err := logutil.NewRuntime(logutil.Config{
				Endpoint:    c.String("otel-logs-endpoint"),
				Protocol:    c.String("otel-logs-protocol"),
				Insecure:    c.Bool("otel-logs-insecure"),
				ServiceName: c.String("otel-service-name"),
				CommandName: c.Command.Name,
				LocalOutput: c.String("local-log-output"),
			})
			if err != nil {
				return err
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = logRuntime.Shutdown(ctx)
			}()
			logger := logRuntime.Logger("e2e-seed")

			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer database.Close()

			// Ensure account exists
			manager := bots.NewManager([]byte(botSeed), nil, nil, nil)
			feedRef, err := manager.GetFeedID(did)
			if err != nil {
				return fmt.Errorf("derive feed: %w", err)
			}

			if err := database.AddBridgedAccount(c.Context, db.BridgedAccount{
				ATDID:     did,
				SSBFeedID: feedRef.Ref(),
				Active:    true,
			}); err != nil {
				return fmt.Errorf("add bridged account: %w", err)
			}
			logger.Printf("ensured bridged account: did=%s feed=%s", did, feedRef.Ref())

			rt, err := ssbruntime.Open(c.Context, ssbruntime.Config{
				RepoPath:   repoPath,
				MasterSeed: []byte(botSeed),
				GossipDB:   nil,
			}, logger)
			if err != nil {
				return fmt.Errorf("open ssb runtime: %w", err)
			}
			defer rt.Close()

			ctx := context.Background()

			// Publish a few test messages
			messages := []map[string]interface{}{
				{
					"type":      "post",
					"text":      fmt.Sprintf("e2e test message 1 — %d", time.Now().UnixNano()),
					"channel":   "e2e-test",
					"createdAt": time.Now().UTC().Format(time.RFC3339),
				},
				{
					"type":      "post",
					"text":      fmt.Sprintf("e2e test message 2 — %d", time.Now().UnixNano()),
					"channel":   "e2e-test",
					"createdAt": time.Now().UTC().Format(time.RFC3339),
				},
				{
					"type":        "about",
					"about":       feedRef.Ref(),
					"name":        "E2E Test Bot",
					"description": "A bot created for e2e Docker testing",
				},
			}

			atURI := func(i int, typ string) string {
				return fmt.Sprintf("at://%s/app.bsky.feed.post/e2e-%d", did, i)
			}

			for i, msg := range messages {
				ref, err := rt.Publish(ctx, did, msg)
				if err != nil {
					return fmt.Errorf("publish msg %d: %w", i, err)
				}
				logger.Printf("published msg %d: ref=%s", i, ref)

				rawJSON, _ := json.Marshal(msg)
				uri := atURI(i, "post")
				if err := database.AddMessage(ctx, db.Message{
					ATURI:        uri,
					ATCID:        fmt.Sprintf("bafy-e2e-%d", i),
					ATDID:        did,
					Type:         "app.bsky.feed.post",
					MessageState: db.MessageStatePublished,
					RawATJson:    string(rawJSON),
					RawSSBJson:   string(rawJSON),
					SSBMsgRef:    ref,
				}); err != nil {
					// Duplicate is fine
					logger.Printf("add message row (may already exist): %v", err)
				}
			}

			if includeBlobPost {
				blobStore := rt.BlobStore()
				blobDir := filepath.Join(repoPath, "blobs")
				if err := os.RemoveAll(blobDir); err != nil {
					logger.Printf("warning: could not clean blob directory: %v", err)
				}
				if err := os.MkdirAll(blobDir, 0755); err != nil {
					logger.Printf("warning: could not create blob directory: %v", err)
				}

				testImageData := []byte{
					0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
					0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
					0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
					0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
					0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xee, 0x00, 0x00,
					0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
				}

				blobHash, err := blobStore.Put(bytes.NewReader(testImageData))
				if err != nil {
					logger.Printf("warning: could not store test blob: %v", err)
				} else {
					blobRef := fmt.Sprintf("&%s.sha256", base64.RawStdEncoding.EncodeToString(blobHash))
					logger.Printf("stored test blob: hash=%x ref=%s", blobHash, blobRef)

					blobPost := map[string]interface{}{
						"type":      "post",
						"text":      fmt.Sprintf("e2e test post with image — %d", time.Now().UnixNano()),
						"mentions":  []map[string]interface{}{{"link": blobRef, "name": "test-image.png"}},
						"createdAt": time.Now().UTC().Format(time.RFC3339),
					}

					ref, err := rt.Publish(ctx, did, blobPost)
					if err != nil {
						logger.Printf("warning: could not publish blob post: %v", err)
					} else {
						logger.Printf("published blob post: ref=%s", ref)

						rawJSON, _ := json.Marshal(blobPost)
						blobURI := fmt.Sprintf("at://%s/app.bsky.feed.post/e2e-blob-%d", did, time.Now().UnixNano())
						if err := database.AddMessage(ctx, db.Message{
							ATURI:        blobURI,
							ATCID:        fmt.Sprintf("bafy-e2e-blob-%d", time.Now().UnixNano()),
							ATDID:        did,
							Type:         "app.bsky.feed.post",
							MessageState: db.MessageStatePublished,
							RawATJson:    string(rawJSON),
							RawSSBJson:   string(rawJSON),
							SSBMsgRef:    ref,
						}); err != nil {
							logger.Printf("add blob message row (may already exist): %v", err)
						}
					}
				}
			} else {
				logger.Printf("skipping blob post seed (--include-blob-post=false)")
			}

			logger.Printf("seeded %d SSB messages for %s (feed: %s)", len(messages), did, feedRef.Ref())
			// Write a marker file so the bridge entrypoint knows seeding is done
			if err := os.WriteFile("/data/seed-complete", []byte("ok"), 0o644); err != nil {
				logger.Printf("warning: could not write seed marker: %v", err)
			}

			return nil
		},
	}

	// Suppress unused import warnings
	_ = io.Discard

	if err := app.RunContext(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
