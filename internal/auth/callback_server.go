package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ErrAuthTimeout is returned when the authorization flow times out.
var ErrAuthTimeout = errors.New("authorization timed out")

// ErrAuthCanceled is returned when the authorization flow is canceled.
var ErrAuthCanceled = errors.New("authorization canceled")

// CallbackResult contains the result of an OAuth callback.
type CallbackResult struct {
	Code  string // The authorization code
	State string // The state parameter for CSRF validation
	Error string // Error from the OAuth provider (if any)
}

// CallbackServer listens on a local port for the OAuth callback redirect.
type CallbackServer struct {
	listener net.Listener
	server   *http.Server
	result   chan CallbackResult
	mu       sync.Mutex
	closed   bool
}

// NewCallbackServer creates a new callback server listening on a random available port.
// The server is not started until WaitForCallback is called.
func NewCallbackServer() (*CallbackServer, error) {
	// Listen on a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on random port: %w", err)
	}

	cs := &CallbackServer{
		listener: listener,
		result:   make(chan CallbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", cs.handleCallback)

	cs.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return cs, nil
}

// Port returns the port the server is listening on.
func (cs *CallbackServer) Port() int {
	return cs.listener.Addr().(*net.TCPAddr).Port
}

// RedirectURL returns the full redirect URL to use in the OAuth flow.
func (cs *CallbackServer) RedirectURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", cs.Port())
}

// handleCallback handles the OAuth callback request.
func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	result := CallbackResult{
		Code:  q.Get("code"),
		State: q.Get("state"),
		Error: q.Get("error"),
	}

	// Send result (non-blocking, buffer size 1)
	select {
	case cs.result <- result:
	default:
		// Result already received, ignore duplicate
	}

	// Respond to browser
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if result.Error != "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Authentication Failed</title></head>
<body>
<h1>Authentication Failed</h1>
<p>Error: %s</p>
<p>You can close this window.</p>
</body>
</html>`, result.Error)
	} else if result.Code == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Authentication Failed</title></head>
<body>
<h1>Authentication Failed</h1>
<p>No authorization code received.</p>
<p>You can close this window.</p>
</body>
</html>`)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Authentication Successful</title></head>
<body>
<h1>Authentication Successful!</h1>
<p>You can close this window and return to the terminal.</p>
</body>
</html>`)
	}
}

// WaitForCallback starts the server and waits for a callback.
// It blocks until a callback is received, the context is canceled, or the timeout expires.
// The server is automatically closed after this method returns.
func (cs *CallbackServer) WaitForCallback(ctx context.Context, timeout time.Duration) (CallbackResult, error) {
	// Start serving in background
	go func() {
		if err := cs.server.Serve(cs.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Log error but don't block
		}
	}()

	// Ensure cleanup
	defer cs.Close()

	// Wait for result or timeout
	select {
	case result := <-cs.result:
		return result, nil
	case <-ctx.Done():
		return CallbackResult{}, ErrAuthCanceled
	case <-time.After(timeout):
		return CallbackResult{}, ErrAuthTimeout
	}
}

// Close stops the server and releases resources.
func (cs *CallbackServer) Close() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.closed {
		return nil
	}
	cs.closed = true

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return cs.server.Shutdown(ctx)
}
