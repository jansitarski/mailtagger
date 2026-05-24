package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClientSecret_Installed(t *testing.T) {
	// Desktop app / installed credential type
	data := []byte(`{
		"installed": {
			"client_id": "123-abc.apps.googleusercontent.com",
			"client_secret": "GOCSPX-secret123",
			"redirect_uris": ["http://localhost", "urn:ietf:wg:oauth:2.0:oob"],
			"auth_uri": "https://accounts.google.com/o/oauth2/auth",
			"token_uri": "https://oauth2.googleapis.com/token"
		}
	}`)

	cs, err := ParseClientSecret(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cs.ClientID != "123-abc.apps.googleusercontent.com" {
		t.Errorf("client_id = %q, want %q", cs.ClientID, "123-abc.apps.googleusercontent.com")
	}
	if cs.ClientSecret != "GOCSPX-secret123" {
		t.Errorf("client_secret = %q, want %q", cs.ClientSecret, "GOCSPX-secret123")
	}
	if len(cs.RedirectURIs) != 2 {
		t.Errorf("redirect_uris len = %d, want 2", len(cs.RedirectURIs))
	}
}

func TestParseClientSecret_Web(t *testing.T) {
	// Web application credential type
	data := []byte(`{
		"web": {
			"client_id": "web-123.apps.googleusercontent.com",
			"client_secret": "GOCSPX-websecret",
			"redirect_uris": ["https://example.com/callback"],
			"auth_uri": "https://accounts.google.com/o/oauth2/auth",
			"token_uri": "https://oauth2.googleapis.com/token"
		}
	}`)

	cs, err := ParseClientSecret(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cs.ClientID != "web-123.apps.googleusercontent.com" {
		t.Errorf("client_id = %q, want %q", cs.ClientID, "web-123.apps.googleusercontent.com")
	}
}

func TestParseClientSecret_DefaultURIs(t *testing.T) {
	// Missing auth_uri and token_uri should be defaulted
	data := []byte(`{
		"installed": {
			"client_id": "test.apps.googleusercontent.com",
			"client_secret": "secret",
			"redirect_uris": ["http://localhost"]
		}
	}`)

	cs, err := ParseClientSecret(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cs.AuthURI != "https://accounts.google.com/o/oauth2/auth" {
		t.Errorf("auth_uri = %q, want default", cs.AuthURI)
	}
	if cs.TokenURI != "https://oauth2.googleapis.com/token" {
		t.Errorf("token_uri = %q, want default", cs.TokenURI)
	}
}

func TestParseClientSecret_Errors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "empty object",
			data:    []byte(`{}`),
			wantErr: "missing 'installed' or 'web' credentials",
		},
		{
			name:    "missing client_id",
			data:    []byte(`{"installed": {"client_secret": "s", "redirect_uris": ["x"]}}`),
			wantErr: "missing client_id",
		},
		{
			name:    "missing client_secret",
			data:    []byte(`{"installed": {"client_id": "c", "redirect_uris": ["x"]}}`),
			wantErr: "missing client_secret",
		},
		{
			name:    "missing redirect_uris",
			data:    []byte(`{"installed": {"client_id": "c", "client_secret": "s"}}`),
			wantErr: "missing redirect_uris",
		},
		{
			name:    "empty redirect_uris",
			data:    []byte(`{"installed": {"client_id": "c", "client_secret": "s", "redirect_uris": []}}`),
			wantErr: "missing redirect_uris",
		},
		{
			name:    "invalid JSON",
			data:    []byte(`{not json}`),
			wantErr: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseClientSecret(tt.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadClientSecret(t *testing.T) {
	// Create temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "client_secret.json")

	data := []byte(`{
		"installed": {
			"client_id": "file-test.apps.googleusercontent.com",
			"client_secret": "GOCSPX-file",
			"redirect_uris": ["http://localhost"]
		}
	}`)

	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cs, err := LoadClientSecret(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cs.ClientID != "file-test.apps.googleusercontent.com" {
		t.Errorf("client_id = %q, want %q", cs.ClientID, "file-test.apps.googleusercontent.com")
	}
}

func TestLoadClientSecret_NotFound(t *testing.T) {
	_, err := LoadClientSecret("/nonexistent/path/client_secret.json")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}
