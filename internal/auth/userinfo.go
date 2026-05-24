package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

const userinfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// UserInfo contains the user information from Google's userinfo API.
type UserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Picture       string `json:"picture"`
}

// GetUserInfo retrieves the user's information from Google using the OAuth token.
func GetUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	client := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, "GET", userinfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call userinfo API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo API returned %d: %s", resp.StatusCode, string(body))
	}

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode userinfo response: %w", err)
	}

	if info.Email == "" {
		return nil, fmt.Errorf("email not found in userinfo response")
	}

	return &info, nil
}
