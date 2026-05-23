package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Label represents a cached Gmail label mapping.
type Label struct {
	ID        int64
	AccountID int64
	LabelName string
	LabelID   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GetLabel retrieves a label by account ID and label name.
func (s *Store) GetLabel(accountID int64, labelName string) (*Label, error) {
	var label Label
	err := s.db.QueryRow(`
		SELECT id, account_id, label_name, label_id, created_at, updated_at
		FROM label_cache
		WHERE account_id = ? AND label_name = ?
	`, accountID, labelName).Scan(
		&label.ID, &label.AccountID, &label.LabelName, &label.LabelID,
		&label.CreatedAt, &label.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("label not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get label: %w", err)
	}
	return &label, nil
}

// UpsertLabel inserts or updates a label mapping.
func (s *Store) UpsertLabel(accountID int64, labelName, labelID string) error {
	_, err := s.db.Exec(`
		INSERT INTO label_cache (account_id, label_name, label_id)
		VALUES (?, ?, ?)
		ON CONFLICT (account_id, label_name)
		DO UPDATE SET
			label_id = excluded.label_id,
			updated_at = CURRENT_TIMESTAMP
	`, accountID, labelName, labelID)
	if err != nil {
		return fmt.Errorf("failed to upsert label: %w", err)
	}
	return nil
}

// ListLabels returns all cached labels for an account.
func (s *Store) ListLabels(accountID int64) ([]*Label, error) {
	rows, err := s.db.Query(`
		SELECT id, account_id, label_name, label_id, created_at, updated_at
		FROM label_cache
		WHERE account_id = ?
		ORDER BY label_name
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to list labels: %w", err)
	}
	defer rows.Close()

	var labels []*Label
	for rows.Next() {
		var label Label
		if err := rows.Scan(
			&label.ID, &label.AccountID, &label.LabelName, &label.LabelID,
			&label.CreatedAt, &label.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan label: %w", err)
		}
		labels = append(labels, &label)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating labels: %w", err)
	}

	return labels, nil
}
