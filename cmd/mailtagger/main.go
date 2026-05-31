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
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"

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
		slog.Info("using encryption key from flag/env", "key_hex", hex.EncodeToString(encryptionKey))
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
			slog.Info("using encryption key from config", "key_hex", cfg.EncryptionKey)
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
			slog.Info("using encryption key from config", "key_hex", cfg.EncryptionKey)
		}
	}
	if encKeyBytes == nil {
		encKeyBytes = make([]byte, 32)
		if _, err := rand.Read(encKeyBytes); err != nil {
			return fmt.Errorf("failed to generate encryption key: %w", err)
		}
		slog.Info("generated encryption key for setup session",
			"key_hex", hex.EncodeToString(encKeyBytes))
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
