package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Client wraps the Gmail API service with OAuth2 token management and rate limiting.
type Client struct {
	service     *gmail.Service
	email       string
	rateLimiter *RateLimiter
}

// NewClient creates a new Gmail API client using OAuth2 credentials.
// clientSecretPath points to the OAuth client_secret.json from Google Cloud Console.
// tokenSource provides the OAuth2 token (typically from store).
func NewClient(ctx context.Context, email string, clientSecretPath string, tokenSource oauth2.TokenSource) (*Client, error) {
	// Read the client secret file
	clientSecretData, err := os.ReadFile(clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret file: %w", err)
	}

	// Parse the OAuth2 config from client_secret.json to validate it
	_, err = google.ConfigFromJSON(clientSecretData, gmail.GmailModifyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client secret: %w", err)
	}

	// Require a valid token source for authenticated requests
	if tokenSource == nil {
		return nil, fmt.Errorf("tokenSource is required for authenticated Gmail API access")
	}

	// Create an HTTP client with the token source
	httpClient := oauth2.NewClient(ctx, tokenSource)

	// Create the Gmail service
	service, err := gmail.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}

	return &Client{
		service:     service,
		email:       email,
		rateLimiter: NewRateLimiter(DefaultRateLimiterConfig()),
	}, nil
}

// Service returns the underlying Gmail API service for direct access.
func (c *Client) Service() *gmail.Service {
	return c.service
}

// Email returns the email address associated with this client.
func (c *Client) Email() string {
	return c.email
}

// RateLimiter returns the rate limiter for this client.
// This can be used to configure rate limiting behavior.
func (c *Client) RateLimiter() *RateLimiter {
	return c.rateLimiter
}

// SetRateLimiter allows replacing the default rate limiter with a custom one.
func (c *Client) SetRateLimiter(rl *RateLimiter) {
	c.rateLimiter = rl
}

// TokenFromFile reads an OAuth2 token from a JSON file.
// This is a helper function for loading tokens from disk.
func TokenFromFile(path string) (*oauth2.Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	return &token, nil
}

// SaveToken writes an OAuth2 token to a JSON file.
// This is a helper function for persisting tokens to disk.
func SaveToken(path string, token *oauth2.Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// GetOAuthConfig returns an OAuth2 config from a client_secret.json file.
// This is useful for the initial OAuth flow.
func GetOAuthConfig(clientSecretPath string) (*oauth2.Config, error) {
	data, err := os.ReadFile(clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret file: %w", err)
	}

	config, err := google.ConfigFromJSON(data, gmail.GmailModifyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client secret: %w", err)
	}

	return config, nil
}
