package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"gopkg.in/yaml.v3"

	"github.com/jansitarski/mailtagger/internal/admin"
	"github.com/jansitarski/mailtagger/internal/auth"
	"github.com/jansitarski/mailtagger/internal/classifier"
	"github.com/jansitarski/mailtagger/internal/config"
	internalGmail "github.com/jansitarski/mailtagger/internal/gmail"
	mthttp "github.com/jansitarski/mailtagger/internal/http"
	"github.com/jansitarski/mailtagger/internal/logging"
	"github.com/jansitarski/mailtagger/internal/pipeline"
	"github.com/jansitarski/mailtagger/internal/setup"
	"github.com/jansitarski/mailtagger/internal/store"
)

// version is set at build time via -ldflags
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "mailtagger",
		Short: "Lightweight self-hosted AI Gmail labeler",
		Long: `mailtagger is a Go-based, single-binary replacement for n8n Gmail-labeling workflows.
It polls Gmail, classifies new messages with an LLM, and applies labels.`,
		Version: version,
	}

	rootCmd.SetVersionTemplate("mailtagger {{.Version}}\n")

	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newResetCursorCmd())
	rootCmd.AddCommand(newSetupCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newServeCmd() *cobra.Command {
	var configPath string
	var addr string
	var clientSecretPath string
	var encryptionKeyHex string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the mailtagger HTTP server and worker",
		Long: `Starts the HTTP server (health, metrics, OAuth callback) and the email classification worker.

Use --dry-run to classify emails without applying labels or recording them as processed.
In dry-run mode, the pipeline logs classification results but makes no changes to Gmail or the database.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			addrOverride := ""
			if cmd.Flags().Changed("addr") {
				addrOverride = addr
			}
			return runServe(cmd.Context(), configPath, addrOverride, clientSecretPath, encryptionKeyHex, dryRun)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "/etc/mailtagger/config.yaml", "path to config file")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "HTTP server listen address (overrides config)")
	cmd.Flags().StringVar(&clientSecretPath, "client-secret", "", "path to OAuth client_secret.json (required in normal mode)")
	cmd.Flags().StringVar(&encryptionKeyHex, "encryption-key", "", "32-byte encryption key in hex (or use MAILTAGGER_ENCRYPTION_KEY env var)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "classify emails but don't apply labels or record as processed (log only)")

	return cmd
}

func runServe(ctx context.Context, configPath, addrOverride, clientSecretPath, encryptionKeyHex string, dryRun bool) error {
	// Load configuration first to get log settings
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logging based on config
	logger := logging.Setup(cfg.Log)

	slog.Info("starting mailtagger", "version", version, "config", configPath)

	// Get encryption key (optional in setup mode)
	var encryptionKey []byte
	envKey := os.Getenv("MAILTAGGER_ENCRYPTION_KEY")
	slog.Debug("checking encryption key sources", "flag_set", encryptionKeyHex != "", "env_set", envKey != "")
	if encryptionKeyHex != "" || envKey != "" {
		encryptionKey, err = getEncryptionKey(encryptionKeyHex)
		if err != nil {
			return err
		}
		slog.Debug("using encryption key from flag/env")
	}

	// Open the store
	st, err := store.Open(cfg.Store.Path, 30)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer st.Close()

	// Run migrations
	if err := st.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate store: %w", err)
	}

	// Check for accounts to determine mode
	hasAccounts, err := st.HasAccounts()
	if err != nil {
		return fmt.Errorf("failed to check accounts: %w", err)
	}

	if !hasAccounts {
		return runSetupMode(ctx, cfg, configPath, addrOverride, st, logger)
	}

	// Normal mode - need encryption key and client secret

	// Encryption key: check flag, env, then config
	if encryptionKey == nil {
		if cfg.EncryptionKey != "" {
			encryptionKey, err = getEncryptionKey(cfg.EncryptionKey)
			if err != nil {
				return fmt.Errorf("invalid encryption_key in config: %w", err)
			}
			slog.Debug("using encryption key from config")
		} else {
			return fmt.Errorf("encryption key required: use --encryption-key flag, MAILTAGGER_ENCRYPTION_KEY env var, or encryption_key in config")
		}
	}

	// Client secret: check flag, then config
	if clientSecretPath == "" {
		if cfg.ClientSecretPath != "" {
			clientSecretPath = cfg.ClientSecretPath
		} else {
			return fmt.Errorf("client secret required: use --client-secret flag or client_secret_path in config")
		}
	}

	return runNormalMode(ctx, cfg, addrOverride, clientSecretPath, encryptionKey, st, logger, dryRun)
}

// runSetupMode runs the server in setup wizard mode (no pipeline, serves /setup).
func runSetupMode(ctx context.Context, cfg *config.Config, configPath, addrOverride string, st *store.Store, logger *slog.Logger) error {
	slog.Info("starting in setup mode (no accounts found)")

	// Use existing encryption key from config, or generate a new one
	var encKeyBytes []byte
	if cfg.EncryptionKey != "" {
		var err error
		encKeyBytes, err = hex.DecodeString(cfg.EncryptionKey)
		if err != nil || len(encKeyBytes) != 32 {
			slog.Warn("invalid encryption key in config, generating new one", "error", err)
			encKeyBytes = nil
		} else {
			slog.Debug("using encryption key from config")
		}
	}
	if encKeyBytes == nil {
		encKeyBytes = make([]byte, 32)
		if _, err := rand.Read(encKeyBytes); err != nil {
			return fmt.Errorf("failed to generate encryption key: %w", err)
		}
		slog.Info("generated encryption key for setup session (saved to config on completion)")
	}

	// Generate setup token
	setupToken, err := setup.GenerateToken(logger)
	if err != nil {
		return fmt.Errorf("failed to generate setup token: %w", err)
	}

	// Override addr from flag if set
	httpCfg := cfg.HTTP
	if addrOverride != "" {
		httpCfg.Addr = addrOverride
	}

	// Log the setup token with access instructions
	addr := httpCfg.Addr
	if addr == "" {
		addr = ":8080"
	}
	setupToken.LogToken(addr)

	// Create HTTP server
	srv, err := mthttp.New(httpCfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create http server: %w", err)
	}

	// Create setup handler
	setupHandler := setup.NewHandler(st, logger)

	// Create API handler for setup endpoints
	apiHandler := setup.NewAPIHandler(setup.APIHandlerConfig{
		Store:         st,
		Token:         setupToken,
		Logger:        logger,
		EncryptionKey: encKeyBytes,
		RunningCfg:    cfg,
		ConfigPath:    configPath,
	})

	// Register /setup routes with token middleware
	srv.Router().Route("/setup", func(r chi.Router) {
		r.Use(setupToken.Middleware)
		r.Get("/", setupHandler.ServeHTTP)
		
		// API routes
		r.Route("/api", func(api chi.Router) {
			apiHandler.Routes(api)
		})
		
		// Catch-all for SPA routing
		r.Get("/*", setupHandler.ServeHTTP)
	})

	// Register /healthz (returns healthy but indicates setup mode)
	srv.Router().Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"setup_mode","message":"mailtagger is running in setup mode"}`))
	})

	// Register /metrics (gated by config)
	if httpCfg.MetricsEnabled {
		srv.Router().Handle("/metrics", mthttp.MetricsHandler())
	}

	// Redirect root to /setup
	srv.Router().Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
	})

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start HTTP server
	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- srv.Start()
	}()

	// Wait for shutdown signal or error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
		if err := srv.Shutdown(10 * time.Second); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	case err := <-httpErrCh:
		if err != nil {
			return fmt.Errorf("http server error: %w", err)
		}
	}

	slog.Info("server stopped")
	return nil
}

// runNormalMode runs the server in normal mode (pipeline running, serves app).
func runNormalMode(ctx context.Context, cfg *config.Config, addrOverride, clientSecretPath string, encryptionKey []byte, st *store.Store, logger *slog.Logger, dryRun bool) error {
	accounts, err := st.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}
	slog.Info("starting in normal mode", "accounts", len(accounts))

	// Create LLM model
	llmModel, err := classifier.NewModel(ctx, cfg.LLM)
	if err != nil {
		return fmt.Errorf("failed to create LLM model: %w", err)
	}

	// Convert config categories to classifier categories
	categories := make([]classifier.Category, len(cfg.Categories))
	for i, cat := range cfg.Categories {
		categories[i] = classifier.Category{
			Name:        cat.Name,
			Description: cat.Description,
		}
	}

	// Create classifier
	cls, err := classifier.New(llmModel, categories)
	if err != nil {
		return fmt.Errorf("failed to create classifier: %w", err)
	}
	cls.WithProvider(cfg.LLM.Provider, cfg.LLM.Model)

	// Create Gmail client factory
	gmailFactory := &gmailClientFactory{
		clientSecretPath: clientSecretPath,
		store:            st,
		encryptionKey:    encryptionKey,
	}

	// Create pipeline
	p := pipeline.New(st, cls, gmailFactory, cfg).WithLogger(logger).WithDryRun(dryRun)

	// Override addr from flag only if explicitly set
	httpCfg := cfg.HTTP
	if addrOverride != "" {
		httpCfg.Addr = addrOverride
	}

	// Create HTTP server
	srv, err := mthttp.New(httpCfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create http server: %w", err)
	}

	// Determine poll interval for health checker
	pollInterval := 5 * time.Minute
	if cfg.PollInterval != "" {
		if d, err := time.ParseDuration(cfg.PollInterval); err == nil {
			pollInterval = d
		}
	}

	// Register /healthz
	health := mthttp.NewHealthChecker(pollInterval)
	srv.Router().Get("/healthz", health.Handler())

	// Register /metrics (gated by config)
	if httpCfg.MetricsEnabled {
		srv.Router().Handle("/metrics", mthttp.MetricsHandler())
	}

	// Register /oauth/callback placeholder
	srv.Router().Get("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"oauth not configured"}`, http.StatusServiceUnavailable)
	})

	// Register /admin routes (enabled by default unless explicitly disabled via admin.enabled: false)
	adminEnabled := cfg.Admin.Enabled == nil || *cfg.Admin.Enabled
	if adminEnabled {
		adminHandler := admin.NewHandler(st, logger, dryRun)
		srv.Router().Route("/admin", func(r chi.Router) {
			// Add basic auth if password is configured
			if cfg.Admin.Password != "" {
				r.Use(admin.BasicAuth(cfg.Admin.Password))
			}
			// Serve API
			r.Route("/api", func(api chi.Router) {
				adminHandler.Routes(api)
			})
			// Serve static SPA
			r.Handle("/*", http.StripPrefix("/admin", admin.StaticHandler()))
			r.Handle("/", http.StripPrefix("/admin", admin.StaticHandler()))
		})
		slog.Info("admin dashboard enabled", "path", "/admin", "auth", cfg.Admin.Password != "")
	}

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start HTTP server in background
	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- srv.Start()
	}()

	// Start pipeline in background
	pipelineErrCh := make(chan error, 1)
	go func() {
		pipelineErrCh <- p.Run(ctx)
	}()

	// Wait for shutdown signal or error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
		if err := srv.Shutdown(10 * time.Second); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	case err := <-httpErrCh:
		if err != nil {
			return fmt.Errorf("http server error: %w", err)
		}
	case err := <-pipelineErrCh:
		if err != nil && err != context.Canceled {
			return fmt.Errorf("pipeline error: %w", err)
		}
	}

	slog.Info("server stopped")
	return nil
}

// gmailClientFactory creates Gmail clients for accounts using database-stored tokens.
type gmailClientFactory struct {
	clientSecretPath string
	store            *store.Store
	encryptionKey    []byte
}

func (f *gmailClientFactory) NewClient(ctx context.Context, account *store.Account) (*internalGmail.Client, error) {
	// Read the client secret file for OAuth config
	clientSecretData, err := os.ReadFile(f.clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret: %w", err)
	}

	oauthConfig, err := google.ConfigFromJSON(clientSecretData, gmail.GmailModifyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client secret: %w", err)
	}

	// Create adapters for the existing StoreTokenSource
	storeAdapter := &tokenStoreAdapter{store: f.store}
	cryptoAdapter := &tokenCryptoAdapter{}

	// Use the existing StoreTokenSource from internal/gmail/auth.go
	tokenSource := internalGmail.NewStoreTokenSource(
		ctx,
		account.Email,
		oauthConfig,
		storeAdapter,
		cryptoAdapter,
		f.encryptionKey,
	)

	return internalGmail.NewClient(ctx, account.Email, f.clientSecretPath, tokenSource)
}

// tokenStoreAdapter adapts store.Store to gmail.TokenStore interface.
type tokenStoreAdapter struct {
	store *store.Store
}

func (a *tokenStoreAdapter) GetAccountByEmail(email string) (int64, []byte, error) {
	account, err := a.store.GetAccountByEmail(email)
	if err != nil {
		return 0, nil, err
	}
	return account.ID, account.EncryptedToken, nil
}

func (a *tokenStoreAdapter) UpdateToken(accountID int64, encryptedToken []byte) error {
	return a.store.UpdateToken(accountID, encryptedToken)
}

// tokenCryptoAdapter adapts store crypto functions to gmail.TokenCrypto interface.
type tokenCryptoAdapter struct{}

func (a *tokenCryptoAdapter) EncryptToken(plaintext []byte, key []byte) ([]byte, error) {
	return store.EncryptToken(plaintext, key)
}

func (a *tokenCryptoAdapter) DecryptToken(ciphertext []byte, key []byte) ([]byte, error) {
	return store.DecryptToken(ciphertext, key)
}

func newAuthCmd() *cobra.Command {
	var clientSecretPath string
	var dbPath string
	var encryptionKeyHex string
	var timeout time.Duration
	var manual bool

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate a Gmail account (headless fallback)",
		Long: `Performs OAuth authentication for a Gmail account via CLI.
This is the headless fallback when the web setup wizard is not accessible.
It prints an authorization URL and prompts for the redirect URL after consent.

The account is identified by the email address obtained from Google during
authentication and is stored in the database keyed by that email.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuth(cmd.Context(), authConfig{
				clientSecretPath: clientSecretPath,
				dbPath:           dbPath,
				encryptionKeyHex: encryptionKeyHex,
				timeout:          timeout,
				manual:           manual,
			})
		},
	}

	cmd.Flags().StringVar(&clientSecretPath, "client-secret", "", "path to OAuth client_secret.json file")
	cmd.Flags().StringVar(&dbPath, "db", "/var/lib/mailtagger/state.db", "path to SQLite database")
	cmd.Flags().StringVar(&encryptionKeyHex, "encryption-key", "", "32-byte encryption key in hex (64 chars), or use MAILTAGGER_ENCRYPTION_KEY env var")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "timeout for OAuth flow")
	cmd.Flags().BoolVar(&manual, "manual", false, "use manual paste flow instead of local callback server")
	cmd.MarkFlagRequired("client-secret")

	return cmd
}

type authConfig struct {
	clientSecretPath string
	dbPath           string
	encryptionKeyHex string
	timeout          time.Duration
	manual           bool
}

func runAuth(ctx context.Context, cfg authConfig) error {
	// Validate client secret file exists
	if _, err := os.Stat(cfg.clientSecretPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("client_secret.json file not found: %s", cfg.clientSecretPath)
		}
		return fmt.Errorf("failed to access client_secret.json: %w", err)
	}

	// Get encryption key
	encryptionKey, err := getEncryptionKey(cfg.encryptionKeyHex)
	if err != nil {
		return err
	}

	fmt.Println("mailtagger OAuth Authentication")
	fmt.Println("================================")
	fmt.Println()

	// Parse client secret
	clientSecret, err := auth.LoadClientSecret(cfg.clientSecretPath)
	if err != nil {
		return fmt.Errorf("failed to load client secret: %w", err)
	}

	// Generate state for CSRF protection
	state, err := generateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	// Determine redirect URI and auth code acquisition method
	var authCode string
	var redirectURI string

	if cfg.manual {
		// Manual paste flow - use out-of-band redirect
		fmt.Println("Using manual paste flow.")
		fmt.Println()

		redirectURI = "urn:ietf:wg:oauth:2.0:oob"
		authURL := clientSecret.AuthCodeURL(redirectURI, state)

		fmt.Println("1. Open this URL in your browser:")
		fmt.Println()
		fmt.Println("   ", authURL)
		fmt.Println()

		result, err := auth.ManualCodeInput(os.Stdin, os.Stdout, state)
		if err != nil {
			return fmt.Errorf("failed to get authorization code: %w", err)
		}
		authCode = result.Code

	} else {
		// Local callback server flow
		callbackServer, err := auth.NewCallbackServer()
		if err != nil {
			return fmt.Errorf("failed to start callback server: %w", err)
		}

		redirectURI = callbackServer.RedirectURL()
		authURL := clientSecret.AuthCodeURL(redirectURI, state)

		fmt.Println("1. Open this URL in your browser:")
		fmt.Println()
		fmt.Println("   ", authURL)
		fmt.Println()
		fmt.Printf("2. Waiting for authorization callback on port %d...\n", callbackServer.Port())
		fmt.Println("   (Press Ctrl+C to cancel, or use --manual flag for paste flow)")
		fmt.Println()

		result, err := callbackServer.WaitForCallback(ctx, cfg.timeout)
		if err != nil {
			if err == auth.ErrAuthTimeout {
				return fmt.Errorf("authorization timed out after %s; try --manual flag", cfg.timeout)
			}
			return fmt.Errorf("authorization failed: %w", err)
		}

		if result.Error != "" {
			return fmt.Errorf("OAuth error: %s", result.Error)
		}
		if result.State != state {
			return fmt.Errorf("state mismatch: possible CSRF attack")
		}
		authCode = result.Code
	}

	fmt.Println("Authorization code received. Exchanging for tokens...")

	// Open database
	st, err := store.Open(cfg.dbPath, 30)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer st.Close()

	// Run migrations
	if err := st.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// Exchange code for tokens using the same redirect URI
	oauthConfig := clientSecret.OAuthConfig(redirectURI)
	exchanger := auth.NewTokenExchanger(oauthConfig, st, encryptionKey)
	result, err := exchanger.Exchange(ctx, authCode)
	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	// Print success message
	fmt.Println()
	fmt.Println("================================")
	fmt.Println("Authentication successful!")
	fmt.Println("================================")
	fmt.Println()
	fmt.Printf("  Email:      %s\n", result.Email)
	fmt.Printf("  Account ID: %d\n", result.AccountID)
	if result.IsNewToken {
		fmt.Println("  Status:     New account created")
	} else {
		fmt.Println("  Status:     Existing account updated")
	}
	fmt.Println()
	fmt.Println("The OAuth token has been encrypted and stored in the database.")
	fmt.Println("You can now use this account with 'mailtagger serve'.")

	return nil
}

// getEncryptionKey returns the encryption key from the flag or environment variable.
func getEncryptionKey(keyHex string) ([]byte, error) {
	if keyHex == "" {
		keyHex = os.Getenv("MAILTAGGER_ENCRYPTION_KEY")
	}
	if keyHex == "" {
		return nil, fmt.Errorf("encryption key required: use --encryption-key flag or MAILTAGGER_ENCRYPTION_KEY env var")
	}

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key: must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid encryption key: must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}

	return key, nil
}

// generateState generates a random state string for CSRF protection.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newResetCursorCmd() *cobra.Command {
	var accountFlag string
	var dbPath string
	var clearProcessed bool

	cmd := &cobra.Command{
		Use:   "reset-cursor",
		Short: "Reset the Gmail history cursor to re-process messages",
		Long: `Resets the history_id cursor for an account, causing mailtagger to re-process
messages from the beginning on the next poll cycle. Use --account to specify
which account to reset (by email or numeric ID), or 'all' for all accounts.

WARNING: This may result in duplicate label applications if messages are already labeled.
Use --clear-processed to also remove the processed_messages dedup records.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResetCursor(accountFlag, dbPath, clearProcessed)
		},
	}

	cmd.Flags().StringVar(&accountFlag, "account", "", "account to reset: email address, numeric ID, or 'all'")
	cmd.Flags().StringVar(&dbPath, "db", "/var/lib/mailtagger/state.db", "path to SQLite database")
	cmd.Flags().BoolVar(&clearProcessed, "clear-processed", false, "also delete processed_messages records for the account(s)")
	cmd.MarkFlagRequired("account")

	return cmd
}

func runResetCursor(accountFlag, dbPath string, clearProcessed bool) error {
	// Open database
	st, err := store.Open(dbPath, 30)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer st.Close()

	// Run migrations
	if err := st.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	if accountFlag == "all" {
		return resetAllAccounts(st, clearProcessed)
	}

	return resetSingleAccount(st, accountFlag, clearProcessed)
}

func resetAllAccounts(st *store.Store, clearProcessed bool) error {
	// List accounts first for reporting
	accounts, err := st.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}
	if len(accounts) == 0 {
		fmt.Println("No accounts found in the database.")
		return nil
	}

	// Reset all history IDs
	resetCount, err := st.ResetAllHistoryIDs()
	if err != nil {
		return fmt.Errorf("failed to reset history IDs: %w", err)
	}

	fmt.Printf("Reset history_id for %d account(s):\n", resetCount)
	for _, acc := range accounts {
		fmt.Printf("  - %s (ID: %d)\n", acc.Email, acc.ID)
	}

	if clearProcessed {
		deleted, err := st.DeleteAllProcessedMessages()
		if err != nil {
			return fmt.Errorf("failed to clear processed messages: %w", err)
		}
		fmt.Printf("Cleared %d processed message record(s).\n", deleted)
	}

	fmt.Println("\nThe pipeline will re-bootstrap from the current Gmail history on next poll cycle.")
	return nil
}

func resetSingleAccount(st *store.Store, accountFlag string, clearProcessed bool) error {
	// Try to find account by email first, then by numeric ID
	var account *store.Account
	var err error

	account, err = st.GetAccountByEmail(accountFlag)
	if err == store.ErrAccountNotFound {
		// Try as numeric ID
		var id int64
		if _, parseErr := fmt.Sscanf(accountFlag, "%d", &id); parseErr == nil {
			account, err = st.GetAccount(id)
		}
	}
	if err != nil {
		if err == store.ErrAccountNotFound {
			return fmt.Errorf("account not found: %s", accountFlag)
		}
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Reset history ID
	if err := st.ResetHistoryID(account.ID); err != nil {
		return fmt.Errorf("failed to reset history_id: %w", err)
	}
	fmt.Printf("Reset history_id for account: %s (ID: %d)\n", account.Email, account.ID)

	if clearProcessed {
		deleted, err := st.DeleteProcessedMessages(account.ID)
		if err != nil {
			return fmt.Errorf("failed to clear processed messages: %w", err)
		}
		fmt.Printf("Cleared %d processed message record(s).\n", deleted)
	}

	fmt.Println("\nThe pipeline will re-bootstrap from the current Gmail history on next poll cycle.")
	return nil
}

func newSetupCmd() *cobra.Command {
	var dbPath string
	var configPath string
	var clientSecretPath string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize mailtagger for headless environments (no web wizard)",
		Long: `Prepares the mailtagger environment without the web-based setup wizard.
This is useful for headless servers, Docker containers, or CI environments
where a browser is not available.

This command will:
  1. Initialize the SQLite database and run migrations
  2. Generate an encryption key (if not already in config)
  3. Copy client_secret.json to the config directory (if provided)
  4. Write the config file with the encryption key embedded

After running this command, use 'mailtagger auth' to authenticate Gmail accounts,
then 'mailtagger serve' to start the pipeline.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(dbPath, configPath, clientSecretPath)
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "/var/lib/mailtagger/state.db", "path to SQLite database")
	cmd.Flags().StringVarP(&configPath, "config", "c", "/etc/mailtagger/config.yaml", "path to config file to create/update")
	cmd.Flags().StringVar(&clientSecretPath, "client-secret", "", "path to OAuth client_secret.json to copy into config directory")

	return cmd
}

func runSetup(dbPath, configPath, clientSecretPath string) error {
	fmt.Println("mailtagger Local Setup")
	fmt.Println("======================")
	fmt.Println()

	// 1. Ensure database directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create database directory %s: %w", dbDir, err)
	}

	// 2. Initialize the database
	st, err := store.Open(dbPath, 30)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}
	fmt.Printf("  [ok] Database initialized: %s\n", dbPath)

	// 3. Generate encryption key
	encKey := make([]byte, 32)
	if _, err := rand.Read(encKey); err != nil {
		return fmt.Errorf("failed to generate encryption key: %w", err)
	}
	encKeyHex := hex.EncodeToString(encKey)
	fmt.Printf("  [ok] Encryption key generated\n")

	// 4. Load or create config
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}

	// Check if config already exists
	var cfg *config.Config
	existingCfg, loadErr := config.Load(configPath)
	if loadErr == nil {
		cfg = existingCfg
		// Preserve existing encryption key if set
		if cfg.EncryptionKey != "" {
			encKeyHex = cfg.EncryptionKey
			fmt.Printf("  [ok] Using existing encryption key from config\n")
		} else {
			cfg.EncryptionKey = encKeyHex
		}
	} else {
		// Create a new default config
		cfg = &config.Config{
			LLM: config.LLMConfig{
				Provider:    "openai",
				Model:       "gpt-4-turbo",
				APIKey:      "${OPENAI_API_KEY}",
				Temperature: 0.1,
				MaxTokens:   200,
				Timeout:     "30s",
			},
			Store: config.StoreConfig{
				Type: "sqlite",
				Path: dbPath,
			},
			HTTP: config.HTTPConfig{
				Addr:           ":8080",
				ReadTimeout:    "10s",
				WriteTimeout:   "10s",
				MetricsEnabled: true,
			},
			Log: config.LogConfig{
				Level:  "info",
				Format: "json",
			},
			PollInterval:  "5m",
			EncryptionKey: encKeyHex,
			Categories: []config.Category{
				{
					Name:        "newsletter",
					Label:       "AI/newsletter",
					Description: "Marketing emails, promotional content, newsletters from companies or products.",
				},
				{
					Name:        "receipt",
					Label:       "AI/receipt",
					Description: "Purchase confirmations, order receipts, payment confirmations, invoices.",
				},
				{
					Name:        "notification",
					Label:       "AI/notification",
					Description: "Service notifications, alerts, status updates, automated system messages.",
				},
				{
					Name:        "personal",
					Label:       "AI/personal",
					Description: "Personal correspondence from individuals, family, or friends.",
				},
				{
					Name:        "Others",
					Label:       "AI/others",
					Description: "Emails that do not fit any other category (required fallback).",
				},
			},
		}
	}

	// 5. Copy client_secret.json if provided
	if clientSecretPath != "" {
		srcData, err := os.ReadFile(clientSecretPath)
		if err != nil {
			return fmt.Errorf("failed to read client_secret.json: %w", err)
		}

		destPath := filepath.Join(configDir, "client_secret.json")
		if err := os.WriteFile(destPath, srcData, 0600); err != nil {
			return fmt.Errorf("failed to write client_secret.json: %w", err)
		}
		cfg.ClientSecretPath = destPath
		fmt.Printf("  [ok] Client secret copied to: %s\n", destPath)
	}

	// 6. Ensure store path matches
	cfg.Store.Path = dbPath

	// 7. Write config file
	yamlBytes, err := marshalConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	fmt.Printf("  [ok] Config written to: %s\n", configPath)

	// Print summary
	fmt.Println()
	fmt.Println("======================")
	fmt.Println("Setup complete!")
	fmt.Println("======================")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println()
	fmt.Println("  1. Set your LLM API key:")
	fmt.Printf("       export OPENAI_API_KEY=your-key-here\n")
	fmt.Println()
	if clientSecretPath == "" {
		fmt.Println("  2. Provide your OAuth client_secret.json:")
		fmt.Printf("       mailtagger auth --client-secret /path/to/client_secret.json --db %s --encryption-key %s\n", dbPath, encKeyHex)
	} else {
		fmt.Println("  2. Authenticate a Gmail account:")
		fmt.Printf("       mailtagger auth --client-secret %s --db %s --encryption-key %s\n", cfg.ClientSecretPath, dbPath, encKeyHex)
	}
	fmt.Println()
	fmt.Println("  3. Start the pipeline:")
	fmt.Printf("       mailtagger serve --config %s\n", configPath)
	fmt.Println()
	fmt.Printf("  Encryption key (save this): %s\n", encKeyHex)

	return nil
}

// marshalConfig serializes a Config to YAML bytes.
func marshalConfig(cfg *config.Config) ([]byte, error) {
	type yamlConfig struct {
		LLM              config.LLMConfig   `yaml:"llm"`
		PollInterval     string             `yaml:"poll_interval"`
		IncludeBody      bool               `yaml:"include_body"`
		Store            config.StoreConfig  `yaml:"store"`
		HTTP             config.HTTPConfig   `yaml:"http"`
		Log              config.LogConfig    `yaml:"log"`
		Categories       []config.Category   `yaml:"categories"`
		ClientSecretPath string             `yaml:"client_secret_path,omitempty"`
		EncryptionKey    string             `yaml:"encryption_key"`
	}

	out := yamlConfig{
		LLM:              cfg.LLM,
		PollInterval:     cfg.PollInterval,
		IncludeBody:      cfg.IncludeBody,
		Store:            cfg.Store,
		HTTP:             cfg.HTTP,
		Log:              cfg.Log,
		Categories:       cfg.Categories,
		ClientSecretPath: cfg.ClientSecretPath,
		EncryptionKey:    cfg.EncryptionKey,
	}

	return yaml.Marshal(out)
}
