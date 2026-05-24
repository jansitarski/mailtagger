package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/jansitarski/mailtagger/internal/store"
)

// TokenExchanger handles the OAuth token exchange and storage.
type TokenExchanger struct {
	config        *oauth2.Config
	store         TokenStore
	encryptionKey []byte
}

// TokenStore defines the interface for storing tokens.
type TokenStore interface {
	GetAccountByEmail(email string) (*store.Account, error)
	InsertAccount(email string, encryptedToken []byte) (*store.Account, error)
	UpdateToken(accountID int64, encryptedToken []byte) error
}

// NewTokenExchanger creates a new TokenExchanger.
func NewTokenExchanger(config *oauth2.Config, ts TokenStore, encryptionKey []byte) *TokenExchanger {
	return &TokenExchanger{
		config:        config,
		store:         ts,
		encryptionKey: encryptionKey,
	}
}

// ExchangeResult contains the result of a successful token exchange.
type ExchangeResult struct {
	Email       string // Email address from the token
	AccountID   int64  // Database account ID
	AccessToken string // Access token (for immediate use)
	IsNewToken  bool   // True if this is a new account, false if updated
}

// Exchange exchanges the authorization code for tokens and stores them.
// It retrieves the user's email from Google's userinfo API and stores the
// encrypted token in the database, keyed by email address.
func (e *TokenExchanger) Exchange(ctx context.Context, code string) (*ExchangeResult, error) {
	// Exchange code for token
	token, err := e.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Validate that we received a refresh token (required for offline access)
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token received; ensure OAuth URL uses access_type=offline and prompt=consent")
	}

	// Get email from userinfo API
	userinfo, err := GetUserInfo(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	email := userinfo.Email

	// Serialize token to JSON
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize token: %w", err)
	}

	// Encrypt token
	encrypted, err := store.EncryptToken(tokenJSON, e.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Store or update account
	result := &ExchangeResult{
		Email:       email,
		AccessToken: token.AccessToken,
	}

	existing, err := e.store.GetAccountByEmail(email)
	if err == nil {
		// Update existing account
		if err := e.store.UpdateToken(existing.ID, encrypted); err != nil {
			return nil, fmt.Errorf("failed to update token: %w", err)
		}
		result.AccountID = existing.ID
		result.IsNewToken = false
	} else if errors.Is(err, store.ErrAccountNotFound) {
		// Insert new account
		acc, err := e.store.InsertAccount(email, encrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to store token: %w", err)
		}
		result.AccountID = acc.ID
		result.IsNewToken = true
	} else {
		return nil, fmt.Errorf("failed to lookup account: %w", err)
	}

	return result, nil
}
