package db

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

type Migration struct {
	Version int
	SQL     string
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

func loadMigrations(migrationsDir string) ([]Migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
			continue
		}

		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{
			Version: version,
			SQL:     string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func (db *DB) runMigrations(migrations []Migration) error {
	for _, migration := range migrations {
		if err := db.applyMigration(migration); err != nil {
			return fmt.Errorf("apply migration %d: %w", migration.Version, err)
		}
	}
	return nil
}

func (db *DB) applyMigration(migration Migration) error {
	_, err := db.conn.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP, description TEXT)")
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	var applied int
	err = db.conn.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?",
		migration.Version,
	).Scan(&applied)

	if err != nil {
		return fmt.Errorf("check migration: %w", err)
	}

	if applied > 0 {
		return nil
	}

	_, err = db.conn.Exec(migration.SQL)
	if err != nil {
		return fmt.Errorf("execute migration %d: %w", migration.Version, err)
	}

	_, err = db.conn.Exec(
		"INSERT INTO schema_migrations (version, description) VALUES (?, ?)",
		migration.Version,
		fmt.Sprintf("migration_%d", migration.Version),
	)
	if err != nil {
		return fmt.Errorf("record migration %d: %w", migration.Version, err)
	}

	return nil
}
