package auth

import (
	"encoding/json"
	"fmt"
	"os"
)

// ClientSecret represents the OAuth 2.0 client credentials from a Google Cloud
// client_secret.json file. Google uses two different wrapper keys depending on
// whether it's a "Web application" or "Desktop app" credential type.
type ClientSecret struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
}

// clientSecretFile is the JSON structure of a Google Cloud client_secret.json file.
// It can have either "installed" (Desktop app) or "web" (Web application) as the top-level key.
type clientSecretFile struct {
	Installed *ClientSecret `json:"installed"`
	Web       *ClientSecret `json:"web"`
}

// LoadClientSecret reads and parses a Google OAuth client_secret.json file.
// It supports both "installed" (Desktop app) and "web" (Web application) credential types.
func LoadClientSecret(path string) (*ClientSecret, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read client_secret.json: %w", err)
	}

	return ParseClientSecret(data)
}

// ParseClientSecret parses the JSON content of a Google OAuth client_secret.json file.
func ParseClientSecret(data []byte) (*ClientSecret, error) {
	var f clientSecretFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse client_secret.json: %w", err)
	}

	// Prefer "installed" (Desktop app) for CLI usage, fall back to "web"
	var cs *ClientSecret
	if f.Installed != nil {
		cs = f.Installed
	} else if f.Web != nil {
		cs = f.Web
	} else {
		return nil, fmt.Errorf("client_secret.json missing 'installed' or 'web' credentials")
	}

	// Validate required fields
	if cs.ClientID == "" {
		return nil, fmt.Errorf("client_secret.json missing client_id")
	}
	if cs.ClientSecret == "" {
		return nil, fmt.Errorf("client_secret.json missing client_secret")
	}
	if len(cs.RedirectURIs) == 0 {
		return nil, fmt.Errorf("client_secret.json missing redirect_uris")
	}

	// Set default URIs if not provided
	if cs.AuthURI == "" {
		cs.AuthURI = "https://accounts.google.com/o/oauth2/auth"
	}
	if cs.TokenURI == "" {
		cs.TokenURI = "https://oauth2.googleapis.com/token"
	}

	return cs, nil
}
