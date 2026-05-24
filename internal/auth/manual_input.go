package auth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// ErrNoCodeInURL is returned when the pasted URL doesn't contain an authorization code.
var ErrNoCodeInURL = errors.New("no authorization code in URL")

// ManualCodeInput prompts the user to paste the redirect URL and extracts the authorization code.
// It reads from the provided reader (typically os.Stdin) and validates against the expected state.
func ManualCodeInput(r io.Reader, w io.Writer, expectedState string) (CallbackResult, error) {
	fmt.Fprintln(w, "\n---")
	fmt.Fprintln(w, "After authorizing, you will be redirected to a URL.")
	fmt.Fprintln(w, "If the automatic redirect didn't work, copy the FULL URL from your browser")
	fmt.Fprintln(w, "and paste it here:")
	fmt.Fprint(w, "\nURL: ")

	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return CallbackResult{}, fmt.Errorf("failed to read input: %w", err)
		}
		return CallbackResult{}, errors.New("no input received")
	}

	rawURL := strings.TrimSpace(scanner.Text())
	return ParseCallbackURL(rawURL, expectedState)
}

// ParseCallbackURL parses a callback URL and extracts the authorization code.
// It validates that the state parameter matches the expected state for CSRF protection.
func ParseCallbackURL(rawURL, expectedState string) (CallbackResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return CallbackResult{}, errors.New("empty input")
	}

	// Handle both full URLs and just the query string
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		// Assume it's just the query part or the code itself
		if strings.Contains(rawURL, "=") {
			rawURL = "http://localhost/?" + strings.TrimPrefix(rawURL, "?")
		} else {
			// Assume it's just the code
			return CallbackResult{Code: rawURL}, nil
		}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return CallbackResult{}, fmt.Errorf("invalid URL: %w", err)
	}

	q := u.Query()
	result := CallbackResult{
		Code:  q.Get("code"),
		State: q.Get("state"),
		Error: q.Get("error"),
	}

	// Check for OAuth error
	if result.Error != "" {
		return result, fmt.Errorf("OAuth error: %s", result.Error)
	}

	// Validate we got a code
	if result.Code == "" {
		return result, ErrNoCodeInURL
	}

	// Validate state if provided
	if expectedState != "" && result.State != expectedState {
		return result, fmt.Errorf("state mismatch: expected %q, got %q", expectedState, result.State)
	}

	return result, nil
}
