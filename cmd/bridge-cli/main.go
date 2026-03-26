package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/urfave/cli/v2"
)

var dbPath string

func main() {
	app := &cli.App{
		Name:  "bridge-cli",
		Usage: "Manage the ATProto to SSB bridge",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "db",
				Value:       "bridge.sqlite",
				Usage:       "path to the sqlite database",
				Destination: &dbPath,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "account",
				Usage: "Manage bridged accounts",
				Subcommands: []*cli.Command{
					{
						Name:  "list",
						Usage: "List all bridged accounts",
						Action: func(c *cli.Context) error {
							database, err := db.Open(dbPath)
							if err != nil {
								return err
							}
							defer database.Close()

							accounts, err := database.GetAllBridgedAccounts(c.Context)
							if err != nil {
								return err
							}

							fmt.Printf("Found %d accounts:\n", len(accounts))
							for _, acc := range accounts {
								status := "active"
								if !acc.Active {
									status = "inactive"
								}
								fmt.Printf("- %s (SSB: %s) [%s]\n", acc.ATDID, acc.SSBFeedID, status)
							}
							return nil
						},
					},
					{
						Name:      "add",
						Usage:     "Add a new bridged account",
						ArgsUsage: "<did>",
						Action: func(c *cli.Context) error {
							did := c.Args().First()
							if did == "" {
								return fmt.Errorf("must provide a DID")
							}

							database, err := db.Open(dbPath)
							if err != nil {
								return err
							}
							defer database.Close()

							// TODO: integrate with bot manager to derive actual SSB Feed ID
							// For now, use a placeholder
							acc := db.BridgedAccount{
								ATDID:     did,
								SSBFeedID: "@placeholder.ed25519",
								Active:    true,
							}

							if err := database.AddBridgedAccount(c.Context, acc); err != nil {
								return err
							}

							fmt.Printf("Added account %s\n", did)
							return nil
						},
					},
					{
						Name:      "remove",
						Usage:     "Deactivate a bridged account",
						ArgsUsage: "<did>",
						Action: func(c *cli.Context) error {
							did := c.Args().First()
							if did == "" {
								return fmt.Errorf("must provide a DID")
							}

							database, err := db.Open(dbPath)
							if err != nil {
								return err
							}
							defer database.Close()

							acc, err := database.GetBridgedAccount(c.Context, did)
							if err != nil {
								return err
							}
							if acc == nil {
								return fmt.Errorf("account not found")
							}

							acc.Active = false
							if err := database.AddBridgedAccount(c.Context, *acc); err != nil {
								return err
							}

							fmt.Printf("Deactivated account %s\n", did)
							return nil
						},
					},
				},
			},
			{
				Name:  "stats",
				Usage: "Show bridge statistics",
				Action: func(c *cli.Context) error {
					fmt.Println("Stats: Not implemented yet")
					return nil
				},
			},
			{
				Name:  "start",
				Usage: "Start the bridge engine",
				Action: func(c *cli.Context) error {
					fmt.Println("Starting bridge engine...")
					// TODO: wire up firehose client, mapper, and bot manager
					<-c.Context.Done()
					return nil
				},
			},
		},
	}

	if err := app.RunContext(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
