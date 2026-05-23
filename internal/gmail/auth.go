package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"golang.org/x/oauth2"
)

// TokenStore defines the interface for storing and retrieving OAuth tokens.
type TokenStore interface {
	// GetAccountByEmail retrieves an account by email address.
	// Returns account with encrypted token.
	GetAccountByEmail(email string) (id int64, encryptedToken []byte, err error)
	
	// UpdateToken updates the encrypted token for an account.
	UpdateToken(accountID int64, encryptedToken []byte) error
}

// TokenCrypto defines the interface for encrypting and decrypting tokens.
type TokenCrypto interface {
	EncryptToken(plaintext []byte, key []byte) ([]byte, error)
	DecryptToken(ciphertext []byte, key []byte) ([]byte, error)
}

// StoreTokenSource implements oauth2.TokenSource that retrieves and refreshes
// tokens from an encrypted store. It caches the token in memory to avoid
// repeated store/decrypt operations on every API call.
type StoreTokenSource struct {
	email      string
	config     *oauth2.Config
	store      TokenStore
	crypto     TokenCrypto
	encryptKey []byte
	ctx        context.Context

	mu          sync.Mutex
	cachedToken *oauth2.Token
}

// NewStoreTokenSource creates a token source that manages tokens in the store.
// ctx is used for token refresh operations.
// config is the OAuth2 configuration.
// store provides access to encrypted tokens.
// crypto provides encryption/decryption.
// encryptKey is the AES key for token encryption (16, 24, or 32 bytes).
func NewStoreTokenSource(ctx context.Context, email string, config *oauth2.Config, store TokenStore, crypto TokenCrypto, encryptKey []byte) *StoreTokenSource {
	return &StoreTokenSource{
		email:      email,
		config:     config,
		store:      store,
		crypto:     crypto,
		encryptKey: encryptKey,
		ctx:        ctx,
	}
}

// Token implements oauth2.TokenSource.
// It retrieves the token from cache or store, checks if it needs refresh,
// and refreshes it if necessary, updating the store with the new token.
// This method is safe for concurrent use.
func (s *StoreTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Return cached token if still valid
	if s.cachedToken != nil && s.cachedToken.Valid() {
		return s.cachedToken, nil
	}

	// Get the account from the store
	accountID, encryptedToken, err := s.store.GetAccountByEmail(s.email)
	if err != nil {
		return nil, fmt.Errorf("failed to get account from store: %w", err)
	}

	// Decrypt the token
	tokenJSON, err := s.crypto.DecryptToken(encryptedToken, s.encryptKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt token: %w", err)
	}

	// Parse the token
	var token oauth2.Token
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %w", err)
	}

	// Check if the token needs refresh
	if token.Valid() {
		s.cachedToken = &token
		return &token, nil
	}

	// Token is expired or invalid, refresh it using the provided context
	tokenSource := s.config.TokenSource(s.ctx, &token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Save the refreshed token back to the store
	if err := s.saveToken(accountID, newToken); err != nil {
		return nil, fmt.Errorf("failed to save refreshed token: %w", err)
	}

	// Cache the new token
	s.cachedToken = newToken

	return newToken, nil
}

// saveToken encrypts and saves a token to the store.
func (s *StoreTokenSource) saveToken(accountID int64, token *oauth2.Token) error {
	// Marshal the token to JSON
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	// Encrypt the token
	encryptedToken, err := s.crypto.EncryptToken(tokenJSON, s.encryptKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Update the store
	if err := s.store.UpdateToken(accountID, encryptedToken); err != nil {
		return fmt.Errorf("failed to update token in store: %w", err)
	}

	return nil
}
