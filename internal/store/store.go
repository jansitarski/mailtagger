package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store represents the SQLite database connection and state.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database at the given path and configures it with WAL mode.
// If path is ":memory:", an in-memory database is created.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying sql.DB for direct access if needed.
func (s *Store) DB() *sql.DB {
	return s.db
}
