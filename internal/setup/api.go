package setup

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"gopkg.in/yaml.v3"

	"github.com/jansitarski/mailtagger/internal/auth"
	"github.com/jansitarski/mailtagger/internal/config"
)

// APIHandler handles the /setup/api/* endpoints.
type APIHandler struct {
	store         SetupStore
	token         *Token
	logger        *slog.Logger
	encryptionKey []byte
	runningCfg    *config.Config
	configPath    string // path to the config file being used

	// mu protects mutable state below
	mu                  sync.Mutex
	clientSecret        *ClientSecretData
	clientSecretRaw     []byte // raw JSON bytes for saving to file
	oauthState          string
	redirectURI         string
	authorizedEmail     string
}

// SetupStore defines the store interface needed during setup.
type SetupStore interface {
	AccountChecker
	auth.TokenStore
}

// ClientSecretData represents the parsed client_secret.json structure.
type ClientSecretData struct {
	Web       *OAuthClientConfig `json:"web,omitempty"`
	Installed *OAuthClientConfig `json:"installed,omitempty"`
}

// OAuthClientConfig holds OAuth client configuration.
type OAuthClientConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
	RedirectURIs []string `json:"redirect_uris"`
}

// GetConfig returns the appropriate config (web or installed).
func (c *ClientSecretData) GetConfig() *OAuthClientConfig {
	if c.Web != nil {
		return c.Web
	}
	return c.Installed
}

// APIHandlerConfig holds configuration for the API handler.
type APIHandlerConfig struct {
	Store         SetupStore
	Token         *Token
	Logger        *slog.Logger
	EncryptionKey []byte
	RunningCfg    *config.Config
	ConfigPath    string // path to the config file being used
}

// NewAPIHandler creates a new API handler.
func NewAPIHandler(cfg APIHandlerConfig) *APIHandler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &APIHandler{
		store:         cfg.Store,
		token:         cfg.Token,
		logger:        cfg.Logger,
		encryptionKey: cfg.EncryptionKey,
		runningCfg:    cfg.RunningCfg,
		configPath:    cfg.ConfigPath,
	}
}

// Routes registers the API routes on a router.
func (h *APIHandler) Routes(r interface {
	Post(pattern string, handler http.HandlerFunc)
	Get(pattern string, handler http.HandlerFunc)
}) {
	r.Post("/client-secret", h.handleClientSecret)
	r.Get("/oauth/start", h.handleOAuthStart)
	r.Get("/oauth/callback", h.handleOAuthCallback)
	r.Get("/oauth/status", h.handleOAuthStatus)
	r.Post("/llm/test", h.handleLLMTest)
	r.Post("/complete", h.handleComplete)
}

// handleClientSecret handles uploading and validating client_secret.json.
func (h *APIHandler) handleClientSecret(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var data ClientSecretData
	if err := json.Unmarshal(body, &data); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	cfg := data.GetConfig()
	if cfg == nil {
		h.respondError(w, http.StatusBadRequest, "invalid client_secret.json: missing 'web' or 'installed' key")
		return
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		h.respondError(w, http.StatusBadRequest, "invalid client_secret.json: missing client_id or client_secret")
		return
	}

	// Store the client secret in memory for the OAuth flow
	h.mu.Lock()
	h.clientSecret = &data
	h.clientSecretRaw = body // Store raw bytes for saving to file later
	h.mu.Unlock()

	truncatedID := cfg.ClientID
	if len(truncatedID) > 20 {
		truncatedID = truncatedID[:20]
	}
	h.logger.Info("client secret uploaded", "client_id", truncatedID+"...")

	clientType := "installed"
	if data.Web != nil {
		clientType = "web"
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"client_id": cfg.ClientID,
		"type":      clientType,
	})
}

// handleOAuthStart initiates the OAuth flow.
func (h *APIHandler) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cs := h.clientSecret
	h.mu.Unlock()

	if cs == nil {
		h.respondError(w, http.StatusBadRequest, "client secret not uploaded")
		return
	}

	cfg := cs.GetConfig()

	// Generate state for CSRF protection
	stateToken, err := GenerateToken(nil)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	// Determine redirect URI
	redirectURI := fmt.Sprintf("%s/setup/api/oauth/callback", getBaseURL(r))

	h.mu.Lock()
	h.oauthState = stateToken.Value()
	h.redirectURI = redirectURI
	h.mu.Unlock()

	// Build authorization URL with proper URL encoding
	params := url.Values{
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/userinfo.email"},
		"state":         {stateToken.Value()},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}
	authURL := cfg.AuthURI + "?" + params.Encode()

	h.respondJSON(w, http.StatusOK, map[string]string{
		"auth_url": authURL,
		"state":    stateToken.Value(),
	})
}

// handleOAuthCallback handles the OAuth callback from Google.
func (h *APIHandler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	// Check for error from OAuth provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		h.logger.Error("oauth provider returned error", "error", errParam, "description", desc)
		http.Redirect(w, r, "/setup?oauth_error="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/setup?oauth_error=missing_code", http.StatusFound)
		return
	}

	state := r.URL.Query().Get("state")

	h.mu.Lock()
	expectedState := h.oauthState
	cs := h.clientSecret
	redirectURI := h.redirectURI
	h.mu.Unlock()

	if state == "" || state != expectedState {
		h.logger.Warn("oauth state mismatch", "expected", expectedState, "got", state)
		http.Redirect(w, r, "/setup?oauth_error=state_mismatch", http.StatusFound)
		return
	}

	if cs == nil {
		http.Redirect(w, r, "/setup?oauth_error=no_client_secret", http.StatusFound)
		return
	}

	cfg := cs.GetConfig()

	// Build oauth2 config for token exchange
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.AuthURI,
			TokenURL: cfg.TokenURI,
		},
		RedirectURL: redirectURI,
		Scopes:      []string{"https://www.googleapis.com/auth/gmail.modify", "https://www.googleapis.com/auth/userinfo.email"},
	}

	// Exchange the authorization code for tokens
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	exchanger := auth.NewTokenExchanger(oauthCfg, h.store, h.encryptionKey)
	result, err := exchanger.Exchange(ctx, code)
	if err != nil {
		h.logger.Error("oauth token exchange failed", "error", err)
		http.Redirect(w, r, "/setup?oauth_error="+url.QueryEscape("token_exchange_failed: "+err.Error()), http.StatusFound)
		return
	}

	h.mu.Lock()
	h.authorizedEmail = result.Email
	h.mu.Unlock()

	h.logger.Info("oauth authorization complete", "email", result.Email, "account_id", result.AccountID, "new", result.IsNewToken)

	// Redirect back to setup wizard
	http.Redirect(w, r, "/setup?oauth_success=true", http.StatusFound)
}

// handleOAuthStatus returns the current OAuth status.
func (h *APIHandler) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	email := h.authorizedEmail
	h.mu.Unlock()

	authorized := email != ""
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"authorized": authorized,
		"email":      email,
	})
}

// handleLLMTest tests the LLM connection by making a real API call.
func (h *APIHandler) handleLLMTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
		BaseURL  string `json:"base_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider == "" {
		h.respondError(w, http.StatusBadRequest, "provider is required")
		return
	}

	if req.Provider != "ollama" && req.APIKey == "" {
		h.respondError(w, http.StatusBadRequest, "api_key is required for "+req.Provider)
		return
	}

	if req.Model == "" {
		h.respondError(w, http.StatusBadRequest, "model is required")
		return
	}

	h.logger.Info("LLM test requested", "provider", req.Provider, "model", req.Model)

	// Actually test the connection
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := testLLMConnection(ctx, req.Provider, req.APIKey, req.Model, req.BaseURL); err != nil {
		h.respondJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Connection successful",
	})
}

// testLLMConnection verifies the LLM provider credentials by making a lightweight API call.
func testLLMConnection(ctx context.Context, provider, apiKey, model, baseURL string) error {
	client := &http.Client{Timeout: 15 * time.Second}

	switch provider {
	case "openai":
		endpoint := "https://api.openai.com/v1/models/" + url.PathEscape(model)
		if baseURL != "" {
			endpoint = strings.TrimRight(baseURL, "/") + "/v1/models/" + url.PathEscape(model)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("invalid API key (HTTP 401)")
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("model %q not found (HTTP 404)", model)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status: HTTP %d", resp.StatusCode)
		}

	case "anthropic":
		endpoint := "https://api.anthropic.com/v1/messages"
		if baseURL != "" {
			endpoint = strings.TrimRight(baseURL, "/") + "/v1/messages"
		}
		// Send a minimal request to validate auth
		body := `{"model":"` + model + `","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to build request: %w", err)
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("invalid API key (HTTP 401)")
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("model %q not found (HTTP 404)", model)
		}
		// 200 or 400 (bad request but auth passed) both mean the key works
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("unexpected status: HTTP %d", resp.StatusCode)
		}

	case "gemini":
		endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s?key=%s",
			url.PathEscape(model), url.QueryEscape(apiKey))
		if baseURL != "" {
			endpoint = fmt.Sprintf("%s/v1/models/%s?key=%s",
				strings.TrimRight(baseURL, "/"), url.PathEscape(model), url.QueryEscape(apiKey))
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("model %q not found (HTTP 404)", model)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status: HTTP %d", resp.StatusCode)
		}

	case "ollama":
		endpoint := "http://localhost:11434/api/tags"
		if baseURL != "" {
			endpoint = strings.TrimRight(baseURL, "/") + "/api/tags"
		}
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("cannot reach Ollama at %s: %w", endpoint, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("Ollama returned HTTP %d", resp.StatusCode)
		}

	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}

	return nil
}

// handleComplete saves the configuration and completes setup.
func (h *APIHandler) handleComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientSecret json.RawMessage `json:"clientSecret"`
		LLMProvider  string          `json:"llmProvider"`
		LLMAPIKey    string          `json:"llmApiKey"`
		LLMModel     string          `json:"llmModel"`
		Categories   []struct {
			Name        string `json:"name"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"categories"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.LLMProvider == "" {
		h.respondError(w, http.StatusBadRequest, "LLM provider is required")
		return
	}
	if req.LLMModel == "" {
		h.respondError(w, http.StatusBadRequest, "LLM model is required")
		return
	}
	if len(req.Categories) == 0 {
		h.respondError(w, http.StatusBadRequest, "at least one category is required")
		return
	}

	// Use current running config values as defaults for store/http
	storePath := "/var/lib/mailtagger/state.db"
	httpAddr := ":8080"
	configDir := "."
	if h.runningCfg != nil {
		if h.runningCfg.Store.Path != "" {
			storePath = h.runningCfg.Store.Path
		}
		if h.runningCfg.HTTP.Addr != "" {
			httpAddr = h.runningCfg.HTTP.Addr
		}
	}
	if h.configPath != "" {
		configDir = filepath.Dir(h.configPath)
	}

	// Save client_secret.json to the same directory as the config
	h.mu.Lock()
	clientSecretRaw := h.clientSecretRaw
	h.mu.Unlock()

	clientSecretPath := filepath.Join(configDir, "client_secret.json")
	if len(clientSecretRaw) > 0 {
		if err := os.WriteFile(clientSecretPath, clientSecretRaw, 0600); err != nil {
			h.logger.Error("failed to save client_secret.json", "error", err, "path", clientSecretPath)
			h.respondError(w, http.StatusInternalServerError, "failed to save client_secret.json: "+err.Error())
			return
		}
		h.logger.Info("saved client_secret.json", "path", clientSecretPath)
	}

	// Get encryption key hex
	encKeyHex := hex.EncodeToString(h.encryptionKey)

	// Build the config with all paths
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: req.LLMProvider,
			Model:    req.LLMModel,
			APIKey:   req.LLMAPIKey,
		},
		PollInterval:     "5m",
		ClientSecretPath: clientSecretPath,
		EncryptionKey:    encKeyHex,
		Store: config.StoreConfig{
			Type: "sqlite",
			Path: storePath,
		},
		HTTP: config.HTTPConfig{
			Addr:           httpAddr,
			MetricsEnabled: true,
		},
	}

	for _, cat := range req.Categories {
		cfg.Categories = append(cfg.Categories, config.Category{
			Name:        cat.Name,
			Label:       cat.Label,
			Description: cat.Description,
		})
	}

	// Generate YAML config
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to generate config")
		return
	}

	// Save the config file
	configPath := h.configPath
	if configPath == "" {
		configPath = filepath.Join(configDir, "config.yaml")
	}
	if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
		h.logger.Error("failed to save config.yaml", "error", err, "path", configPath)
		h.respondError(w, http.StatusInternalServerError, "failed to save config.yaml: "+err.Error())
		return
	}
	h.logger.Info("saved config.yaml", "path", configPath, "encryption_key", encKeyHex)

	h.logger.Info("setup complete",
		"provider", req.LLMProvider,
		"model", req.LLMModel,
		"categories", len(req.Categories),
		"config_path", configPath,
		"client_secret_path", clientSecretPath,
	)

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":             "ok",
		"message":            "Setup complete! Configuration saved. Restart mailtagger to begin processing emails.",
		"config":             string(yamlBytes),
		"config_path":        configPath,
		"client_secret_path": clientSecretPath,
		"encryption_key":     encKeyHex,
	})
}

func (h *APIHandler) respondJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func (h *APIHandler) respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// getBaseURL extracts the base URL from the request.
func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Check for X-Forwarded-Proto header (behind proxy)
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

// SanitizeAPIKey masks an API key for logging (shows first/last 4 chars).
func SanitizeAPIKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

// EncryptionKeyHex returns the hex-encoded encryption key for display.
func (h *APIHandler) EncryptionKeyHex() string {
	return hex.EncodeToString(h.encryptionKey)
}
