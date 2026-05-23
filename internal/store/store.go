package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// Store represents the SQLite database connection and state.
type Store struct {
	db              *sql.DB
	gcCtx           context.Context
	gcCancel        context.CancelFunc
	retentionDays   int
}

// Open opens a SQLite database at the given path and configures it with WAL mode.
// If path is ":memory:", an in-memory database is created.
// retentionDays specifies how many days of processed messages to keep (default: 30).
func Open(path string, retentionDays int) (*Store, error) {
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

	if retentionDays <= 0 {
		retentionDays = 30
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{
		db:            db,
		gcCtx:         ctx,
		gcCancel:      cancel,
		retentionDays: retentionDays,
	}

	// Start the GC goroutine
	go s.gcLoop()

	return s, nil
}

// Close closes the database connection and stops the GC goroutine.
func (s *Store) Close() error {
	if s.gcCancel != nil {
		s.gcCancel()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying sql.DB for direct access if needed.
func (s *Store) DB() *sql.DB {
	return s.db
}

// gcLoop runs the garbage collection loop for processed messages.
// It runs every hour and deletes messages older than the retention period.
func (s *Store) gcLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Run immediately on startup
	s.runGC()

	for {
		select {
		case <-s.gcCtx.Done():
			return
		case <-ticker.C:
			s.runGC()
		}
	}
}

// runGC performs a single garbage collection run.
func (s *Store) runGC() {
	deleted, err := s.GarbageCollectProcessedMessages(s.retentionDays)
	if err != nil {
		log.Printf("Error during GC: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("GC: deleted %d old processed messages (retention: %d days)", deleted, s.retentionDays)
	}
}
