package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jansitarski/mailtagger/internal/config"
	mthttp "github.com/jansitarski/mailtagger/internal/http"
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

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the mailtagger HTTP server and worker",
		Long:  `Starts the HTTP server (health, metrics, OAuth callback) and the email classification worker.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), configPath, addr)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "/etc/mailtagger/config.yaml", "path to config file")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "HTTP server listen address")

	return cmd
}

func runServe(ctx context.Context, configPath, addr string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("starting mailtagger", "version", version, "config", configPath, "addr", addr)

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override addr from flag if provided
	httpCfg := cfg.HTTP
	if addr != "" {
		httpCfg.Addr = addr
	}

	// Create HTTP server
	srv, err := mthttp.New(httpCfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create http server: %w", err)
	}

	// Determine poll interval for health checker
	pollInterval := 5 * time.Minute
	if len(cfg.Accounts) > 0 && cfg.Accounts[0].PollInterval != "" {
		if d, err := time.ParseDuration(cfg.Accounts[0].PollInterval); err == nil {
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

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start HTTP server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
		if err := srv.Shutdown(10 * time.Second); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	slog.Info("server stopped")
	return nil
}

func newAuthCmd() *cobra.Command {
	var accountID string
	var clientSecretPath string

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate a Gmail account (headless fallback)",
		Long: `Performs OAuth authentication for a Gmail account via CLI.
This is the headless fallback when the web setup wizard is not accessible.
It prints an authorization URL and prompts for the redirect URL after consent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuth(accountID, clientSecretPath)
		},
	}

	cmd.Flags().StringVar(&accountID, "account", "primary", "account ID to authenticate")
	cmd.Flags().StringVar(&clientSecretPath, "client-secret", "", "path to OAuth client_secret.json file")
	cmd.MarkFlagRequired("client-secret")

	return cmd
}

func runAuth(accountID, clientSecretPath string) error {
	slog.Info("auth command placeholder", "account", accountID, "client_secret", clientSecretPath)
	fmt.Println("OAuth authentication flow not yet implemented.")
	fmt.Println("This will:")
	fmt.Println("  1. Read OAuth credentials from", clientSecretPath)
	fmt.Println("  2. Generate an authorization URL")
	fmt.Println("  3. Prompt you to paste the redirect URL after consent")
	fmt.Println("  4. Exchange the code for tokens and store them")
	return nil
}

func newResetCursorCmd() *cobra.Command {
	var accountID string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "reset-cursor",
		Short: "Reset the Gmail history cursor to re-process messages",
		Long: `Resets the history_id cursor for an account, causing mailtagger to re-process
messages from the beginning. Use this if you want to re-classify all emails.
WARNING: This may result in duplicate label applications if messages are already labeled.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResetCursor(accountID, dbPath)
		},
	}

	cmd.Flags().StringVar(&accountID, "account", "", "account ID to reset (required, or 'all' for all accounts)")
	cmd.Flags().StringVar(&dbPath, "db", "/var/lib/mailtagger/state.db", "path to SQLite database")
	cmd.MarkFlagRequired("account")

	return cmd
}

func runResetCursor(accountID, dbPath string) error {
	slog.Info("reset-cursor command placeholder", "account", accountID, "db", dbPath)
	fmt.Println("Cursor reset not yet implemented.")
	fmt.Println("This will:")
	fmt.Println("  1. Open the database at", dbPath)
	fmt.Println("  2. Reset history_id for account:", accountID)
	fmt.Println("  3. Optionally clear processed_messages for the account")
	return nil
}
