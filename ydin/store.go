// Package store manages the SQLite database (WAL mode) for MeshCommons.
package store

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// DB wraps *sql.DB with domain helpers.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite file at path with WAL journal mode.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000", path)
	raw, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := raw.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	// Limit writer concurrency to 1; SQLite WAL allows concurrent readers.
	raw.SetMaxOpenConns(1)
	return &DB{raw}, nil
}

// Migrate applies the embedded DDL schema to the database.
// It is idempotent (IF NOT EXISTS everywhere).
func Migrate(db *DB) error {
	ddl := []string{
		ddlMessages,
		ddlFiles,
		ddlPeers,
		ddlWikiPages,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}

// ── DDL statements ────────────────────────────────────────────────────────

const ddlMessages = `
CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    mesh_id     TEXT    NOT NULL,          -- Meshtastic packet ID
    from_node   TEXT    NOT NULL,
    to_node     TEXT    NOT NULL DEFAULT 'broadcast',
    channel     INTEGER NOT NULL DEFAULT 0,
    payload     BLOB    NOT NULL,
    received_at INTEGER NOT NULL,          -- Unix milliseconds
    synced      INTEGER NOT NULL DEFAULT 0 -- bool: 0 = pending, 1 = synced
);
CREATE INDEX IF NOT EXISTS idx_messages_received_at ON messages (received_at DESC);
`

const ddlFiles = `
CREATE TABLE IF NOT EXISTS files (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    info_hash   TEXT    NOT NULL UNIQUE,  -- BitTorrent info-hash (hex)
    name        TEXT    NOT NULL,
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    added_at    INTEGER NOT NULL,         -- Unix seconds
    seeding     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_files_info_hash ON files (info_hash);
`

const ddlPeers = `
CREATE TABLE IF NOT EXISTS peers (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      TEXT    NOT NULL UNIQUE,
    display_name TEXT,
    last_seen    INTEGER NOT NULL,        -- Unix seconds
    transport    TEXT    NOT NULL DEFAULT 'mesh' -- 'mesh' | 'tcp' | 'ble'
);
`

const ddlWikiPages = `
CREATE TABLE IF NOT EXISTS wiki_pages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT    NOT NULL UNIQUE,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wiki_pages_slug ON wiki_pages (slug);
`
