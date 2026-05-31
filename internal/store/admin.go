package store

import (
	"fmt"
	"time"
)

// AccountStats contains account info plus aggregated statistics.
type AccountStats struct {
	ID               int64     `json:"id"`
	Email            string    `json:"email"`
	HistoryID        string    `json:"history_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	ProcessedCount   int64     `json:"processed_count"`
	LastProcessedAt  *time.Time `json:"last_processed_at"`
}

// ProcessedMessageRecord represents a processed message with metadata for display.
type ProcessedMessageRecord struct {
	ID          int64     `json:"id"`
	AccountID   int64     `json:"account_id"`
	Email       string    `json:"email"`
	MessageID   string    `json:"message_id"`
	ProcessedAt time.Time `json:"processed_at"`
}

// PipelineStatus contains overall pipeline health information.
type PipelineStatus struct {
	TotalAccounts    int            `json:"total_accounts"`
	TotalProcessed   int64          `json:"total_processed"`
	AccountStatuses  []AccountStats `json:"account_statuses"`
}

// ListAccountStats returns all accounts with their processing statistics.
func (s *Store) ListAccountStats() ([]AccountStats, error) {
	rows, err := s.db.Query(`
		SELECT
			a.id,
			a.email,
			a.history_id,
			a.created_at,
			a.updated_at,
			COALESCE(pm.cnt, 0) AS processed_count,
			pm.last_processed
		FROM accounts a
		LEFT JOIN (
			SELECT account_id,
				COUNT(*) AS cnt,
				MAX(processed_at) AS last_processed
			FROM processed_messages
			GROUP BY account_id
		) pm ON pm.account_id = a.id
		ORDER BY a.email
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list account stats: %w", err)
	}
	defer rows.Close()

	var stats []AccountStats
	for rows.Next() {
		var s AccountStats
		if err := rows.Scan(
			&s.ID, &s.Email, &s.HistoryID, &s.CreatedAt, &s.UpdatedAt,
			&s.ProcessedCount, &s.LastProcessedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan account stats: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating account stats: %w", err)
	}
	return stats, nil
}

// GetRecentProcessedMessages returns the most recent processed messages with pagination.
// limit is the max number of records to return, offset is the starting position.
// If accountID is > 0, filters to that account only.
func (s *Store) GetRecentProcessedMessages(accountID int64, limit, offset int) ([]ProcessedMessageRecord, int64, error) {
	// Get total count
	var total int64
	var countQuery string
	var countArgs []interface{}
	if accountID > 0 {
		countQuery = `SELECT COUNT(*) FROM processed_messages WHERE account_id = ?`
		countArgs = append(countArgs, accountID)
	} else {
		countQuery = `SELECT COUNT(*) FROM processed_messages`
	}
	if err := s.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count processed messages: %w", err)
	}

	// Get paginated records
	var query string
	var args []interface{}
	if accountID > 0 {
		query = `
			SELECT pm.id, pm.account_id, a.email, pm.message_id, pm.processed_at
			FROM processed_messages pm
			JOIN accounts a ON a.id = pm.account_id
			WHERE pm.account_id = ?
			ORDER BY pm.processed_at DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{accountID, limit, offset}
	} else {
		query = `
			SELECT pm.id, pm.account_id, a.email, pm.message_id, pm.processed_at
			FROM processed_messages pm
			JOIN accounts a ON a.id = pm.account_id
			ORDER BY pm.processed_at DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{limit, offset}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query processed messages: %w", err)
	}
	defer rows.Close()

	var records []ProcessedMessageRecord
	for rows.Next() {
		var r ProcessedMessageRecord
		if err := rows.Scan(&r.ID, &r.AccountID, &r.Email, &r.MessageID, &r.ProcessedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan processed message: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating processed messages: %w", err)
	}

	return records, total, nil
}

// GetPipelineStatus returns overall pipeline health information.
func (s *Store) GetPipelineStatus() (*PipelineStatus, error) {
	stats, err := s.ListAccountStats()
	if err != nil {
		return nil, err
	}

	var totalProcessed int64
	for _, s := range stats {
		totalProcessed += s.ProcessedCount
	}

	return &PipelineStatus{
		TotalAccounts:   len(stats),
		TotalProcessed:  totalProcessed,
		AccountStatuses: stats,
	}, nil
}
