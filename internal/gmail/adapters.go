package gmail

import (
	"fmt"
)

// StoreAdapter adapts a store implementation to the TokenStore interface.
type StoreAdapter struct {
	getAccountByEmail func(email string) (id int64, encryptedToken []byte, err error)
	updateToken       func(accountID int64, encryptedToken []byte) error
}

// NewStoreAdapter creates a store adapter from function implementations.
func NewStoreAdapter(
	getAccountByEmail func(email string) (id int64, encryptedToken []byte, err error),
	updateToken func(accountID int64, encryptedToken []byte) error,
) *StoreAdapter {
	return &StoreAdapter{
		getAccountByEmail: getAccountByEmail,
		updateToken:       updateToken,
	}
}

// GetAccountByEmail implements TokenStore.
func (a *StoreAdapter) GetAccountByEmail(email string) (int64, []byte, error) {
	if a.getAccountByEmail == nil {
		return 0, nil, fmt.Errorf("getAccountByEmail function not set")
	}
	return a.getAccountByEmail(email)
}

// UpdateToken implements TokenStore.
func (a *StoreAdapter) UpdateToken(accountID int64, encryptedToken []byte) error {
	if a.updateToken == nil {
		return fmt.Errorf("updateToken function not set")
	}
	return a.updateToken(accountID, encryptedToken)
}

// CryptoAdapter adapts crypto functions to the TokenCrypto interface.
type CryptoAdapter struct {
	encryptToken func(plaintext []byte, key []byte) ([]byte, error)
	decryptToken func(ciphertext []byte, key []byte) ([]byte, error)
}

// NewCryptoAdapter creates a crypto adapter from function implementations.
func NewCryptoAdapter(
	encryptToken func(plaintext []byte, key []byte) ([]byte, error),
	decryptToken func(ciphertext []byte, key []byte) ([]byte, error),
) *CryptoAdapter {
	return &CryptoAdapter{
		encryptToken: encryptToken,
		decryptToken: decryptToken,
	}
}

// EncryptToken implements TokenCrypto.
func (a *CryptoAdapter) EncryptToken(plaintext []byte, key []byte) ([]byte, error) {
	if a.encryptToken == nil {
		return nil, fmt.Errorf("encrypt function not set")
	}
	return a.encryptToken(plaintext, key)
}

// DecryptToken implements TokenCrypto.
func (a *CryptoAdapter) DecryptToken(ciphertext []byte, key []byte) ([]byte, error) {
	if a.decryptToken == nil {
		return nil, fmt.Errorf("decrypt function not set")
	}
	return a.decryptToken(ciphertext, key)
}
