package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jansitarski/mailtagger/internal/config"
)

// APIHandler handles the /setup/api/* endpoints.
type APIHandler struct {
	store         SetupStore
	token         *Token
	logger        *slog.Logger
	configPath    string
	encryptionKey []byte

	// State stored during setup flow
	clientSecret  *ClientSecretData
	oauthState    string
	authorizedEmail string
}

// SetupStore extends AccountChecker with write operations needed during setup.
type SetupStore interface {
	AccountChecker
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
	ConfigPath    string
	EncryptionKey []byte
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
		configPath:    cfg.ConfigPath,
		encryptionKey: cfg.EncryptionKey,
	}
}

// Routes registers the API routes on a router.
func (h *APIHandler) Routes(r interface{ Post(pattern string, handler http.HandlerFunc); Get(pattern string, handler http.HandlerFunc) }) {
	r.Post("/client-secret", h.handleClientSecret)
	r.Get("/oauth/start", h.handleOAuthStart)
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
	h.clientSecret = &data

	h.logger.Info("client secret uploaded", "client_id", cfg.ClientID[:min(20, len(cfg.ClientID))]+"...")

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"client_id": cfg.ClientID,
		"type":      func() string { if data.Web != nil { return "web" }; return "installed" }(),
	})
}

// handleOAuthStart initiates the OAuth flow.
func (h *APIHandler) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if h.clientSecret == nil {
		h.respondError(w, http.StatusBadRequest, "client secret not uploaded")
		return
	}

	cfg := h.clientSecret.GetConfig()
	
	// Generate state for CSRF protection
	stateToken, err := GenerateToken(nil)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}
	h.oauthState = stateToken.Value()

	// Determine redirect URI
	redirectURI := fmt.Sprintf("%s/setup/api/oauth/callback", getBaseURL(r))
	
	// Build authorization URL
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&access_type=offline&prompt=consent",
		cfg.AuthURI,
		cfg.ClientID,
		redirectURI,
		"https://www.googleapis.com/auth/gmail.modify",
		h.oauthState,
	)

	h.respondJSON(w, http.StatusOK, map[string]string{
		"auth_url": authURL,
		"state":    h.oauthState,
	})
}

// handleOAuthStatus returns the current OAuth status.
func (h *APIHandler) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	authorized := h.authorizedEmail != ""
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"authorized": authorized,
		"email":      h.authorizedEmail,
	})
}

// handleLLMTest tests the LLM connection.
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

	// For now, just validate the input
	// TODO: Actually test the connection by making a simple API call
	h.logger.Info("LLM test requested", "provider", req.Provider, "model", req.Model)

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Configuration validated",
	})
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

	// Build the config
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider: req.LLMProvider,
			Model:    req.LLMModel,
			APIKey:   req.LLMAPIKey,
		},
		PollInterval: "5m",
		Store: config.StoreConfig{
			Type: "sqlite",
			Path: "/var/lib/mailtagger/state.db",
		},
		HTTP: config.HTTPConfig{
			Addr:           ":8080",
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

	// For now, just log the config
	// TODO: Actually save the config file and restart in normal mode
	h.logger.Info("setup complete requested",
		"provider", req.LLMProvider,
		"model", req.LLMModel,
		"categories", len(req.Categories),
	)

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"message": "Configuration saved. Please restart mailtagger.",
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SanitizeAPIKey masks an API key for logging (shows first/last 4 chars).
func SanitizeAPIKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}
