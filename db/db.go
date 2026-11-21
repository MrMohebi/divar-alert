// Package db stores alerts, drafts, deliveries, and AI history in SQLite.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// SchemaVersion is stored in PRAGMA user_version. Databases written by other
// versions are refused instead of migrated.
const SchemaVersion = 3

// maxHistoryPosts bounds the AI comparison history kept per alert.
const maxHistoryPosts = 50

const schema = `
CREATE TABLE alerts (
	id INTEGER PRIMARY KEY,
	platform TEXT NOT NULL CHECK(platform IN ('telegram', 'bale')),
	chat_id INTEGER NOT NULL,
	title TEXT NOT NULL,
	search_query TEXT NOT NULL CHECK(json_valid(search_query)),
	original_prompt TEXT NOT NULL DEFAULT '',
	ai_enabled BOOLEAN NOT NULL DEFAULT true,
	analysis_prompt TEXT NOT NULL DEFAULT '',
	interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 10 AND 86400),
	last_checked INTEGER NOT NULL DEFAULT 0,
	watch_state TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(watch_state)),
	generation INTEGER NOT NULL DEFAULT 0,
	deleted_at INTEGER
);
CREATE INDEX idx_alerts_chat ON alerts(platform, chat_id) WHERE deleted_at IS NULL;

-- One row per successful notification. A reset retires the rows of the
-- alert's current generation but keeps them for auditing.
CREATE TABLE deliveries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	alert_id INTEGER NOT NULL REFERENCES alerts(id),
	token TEXT NOT NULL,
	data TEXT CHECK(data IS NULL OR json_valid(data)),
	created_at INTEGER NOT NULL,
	retired_at INTEGER
);
CREATE UNIQUE INDEX idx_active_delivery ON deliveries(alert_id, token) WHERE retired_at IS NULL;

CREATE TABLE alert_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	data TEXT NOT NULL CHECK(json_valid(data)),
	created_at INTEGER NOT NULL
);
CREATE INDEX idx_alert_history_alert ON alert_history(alert_id, id);

-- The unfinished conversational alert of each chat.
CREATE TABLE drafts (
	platform TEXT NOT NULL CHECK(platform IN ('telegram', 'bale')),
	chat_id INTEGER NOT NULL,
	data TEXT NOT NULL CHECK(json_valid(data)),
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (platform, chat_id)
);
`

// DB wraps the SQLite connection.
type DB struct {
	conn *sql.DB
	// alertLocks serializes delivery with delete and reset per alert.
	alertLocks sync.Map
}

// Open opens the database at path, creating the schema in a new database.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("configure sqlite database: %w", err)
		}
	}
	if err := initSchema(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return &DB{conn: conn}, nil
}

func initSchema(conn *sql.DB) error {
	var version, tables int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version == SchemaVersion {
		return nil
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	if version != 0 || tables != 0 {
		return fmt.Errorf("database has schema version %d, this version needs %d; start with a new DB_PATH", version, SchemaVersion)
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion)); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return tx.Commit()
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// requireAffected turns an update that matched no row into sql.ErrNoRows.
func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
