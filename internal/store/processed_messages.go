package store

import (
	"fmt"
	"time"
)

// ProcessedMessage represents a message that has been processed.
type ProcessedMessage struct {
	ID          int64
	AccountID   int64
	MessageID   string
	ProcessedAt time.Time
}

// InsertProcessedMessage records that a message has been processed.
func (s *Store) InsertProcessedMessage(accountID int64, messageID string) error {
	_, err := s.db.Exec(`
		INSERT INTO processed_messages (account_id, message_id)
		VALUES (?, ?)
		ON CONFLICT (account_id, message_id) DO NOTHING
	`, accountID, messageID)
	if err != nil {
		return fmt.Errorf("failed to insert processed message: %w", err)
	}
	return nil
}

// ProcessedMessageExists checks if a message has already been processed.
func (s *Store) ProcessedMessageExists(accountID int64, messageID string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM processed_messages
			WHERE account_id = ? AND message_id = ?
		)
	`, accountID, messageID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check processed message: %w", err)
	}
	return exists, nil
}

// GarbageCollectProcessedMessages deletes processed messages older than the retention period.
// retentionDays specifies how many days of processed messages to keep.
func (s *Store) GarbageCollectProcessedMessages(retentionDays int) (int64, error) {
	result, err := s.db.Exec(`
		DELETE FROM processed_messages
		WHERE processed_at < datetime('now', '-' || ? || ' days')
	`, retentionDays)
	if err != nil {
		return 0, fmt.Errorf("failed to garbage collect processed messages: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}
