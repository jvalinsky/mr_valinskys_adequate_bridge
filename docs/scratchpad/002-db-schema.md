# Database Schema

- Created SQLite database schema in `internal/db/schema.sql`
- Contains tables:
  - `bridged_accounts` (maps AT DID to SSB feed ID)
  - `messages` (maps AT URI to SSB message ref, stores raw JSON for admin UI side-by-side view)
  - `blobs` (maps AT CIDs to SSB blob refs)
- Implemented `db.go` with core CRUD operations and memory/sqlite3 driver.
- Wrote tests in `db_test.go` and verified they pass.