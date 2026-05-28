// Package logging provides slog configuration based on environment and config.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/jansitarski/mailtagger/internal/config"
)

// Setup configures slog with the given log config and returns the configured logger.
// It also sets the default logger.
//
// Level options: "debug", "info", "warn", "error" (default: "info")
// Format options: "json", "text" (default: "json")
func Setup(cfg config.LogConfig) *slog.Logger {
	return SetupWriter(cfg, os.Stdout)
}

// SetupWriter configures slog with the given log config and writer.
// Useful for testing.
func SetupWriter(cfg config.LogConfig, w io.Writer) *slog.Logger {
	level := parseLevel(cfg.Level)
	format := strings.ToLower(strings.TrimSpace(cfg.Format))

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		// Default to JSON for production
		handler = slog.NewJSONHandler(w, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger
}

// parseLevel converts a string log level to slog.Level.
// Defaults to Info if not recognized.
func parseLevel(levelStr string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
