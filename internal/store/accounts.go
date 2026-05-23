package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Account represents a Gmail account stored in the database.
type Account struct {
	ID             int64
	Email          string
	EncryptedToken []byte
	HistoryID      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// InsertAccount inserts a new account into the database.
func (s *Store) InsertAccount(email string, encryptedToken []byte) (*Account, error) {
	result, err := s.db.Exec(`
		INSERT INTO accounts (email, encrypted_token, history_id)
		VALUES (?, ?, '')
	`, email, encryptedToken)
	if err != nil {
		return nil, fmt.Errorf("failed to insert account: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return s.GetAccount(id)
}

// GetAccount retrieves an account by ID.
func (s *Store) GetAccount(id int64) (*Account, error) {
	var acc Account
	err := s.db.QueryRow(`
		SELECT id, email, encrypted_token, history_id, created_at, updated_at
		FROM accounts
		WHERE id = ?
	`, id).Scan(&acc.ID, &acc.Email, &acc.EncryptedToken, &acc.HistoryID, &acc.CreatedAt, &acc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return &acc, nil
}

// GetAccountByEmail retrieves an account by email address.
func (s *Store) GetAccountByEmail(email string) (*Account, error) {
	var acc Account
	err := s.db.QueryRow(`
		SELECT id, email, encrypted_token, history_id, created_at, updated_at
		FROM accounts
		WHERE email = ?
	`, email).Scan(&acc.ID, &acc.Email, &acc.EncryptedToken, &acc.HistoryID, &acc.CreatedAt, &acc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return &acc, nil
}

// ListAccounts returns all accounts in the database.
func (s *Store) ListAccounts() ([]*Account, error) {
	rows, err := s.db.Query(`
		SELECT id, email, encrypted_token, history_id, created_at, updated_at
		FROM accounts
		ORDER BY email
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*Account
	for rows.Next() {
		var acc Account
		if err := rows.Scan(&acc.ID, &acc.Email, &acc.EncryptedToken, &acc.HistoryID, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}
		accounts = append(accounts, &acc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating accounts: %w", err)
	}

	return accounts, nil
}

// UpdateHistoryID updates the history_id for an account.
func (s *Store) UpdateHistoryID(accountID int64, historyID string) error {
	_, err := s.db.Exec(`
		UPDATE accounts
		SET history_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, historyID, accountID)
	if err != nil {
		return fmt.Errorf("failed to update history_id: %w", err)
	}
	return nil
}
