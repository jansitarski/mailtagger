package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jansitarski/mailtagger/internal/config"
)

func TestSetupWriter_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "info",
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Info("test message", "key", "value")

	// Verify JSON output
	output := buf.String()
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Errorf("expected JSON output with msg field, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("expected JSON output with key field, got: %s", output)
	}

	// Verify it's valid JSON
	var jsonMap map[string]interface{}
	if err := json.Unmarshal([]byte(output), &jsonMap); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

func TestSetupWriter_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "info",
		Format: "text",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Info("test message", "key", "value")

	output := buf.String()
	// Text format uses key=value pairs
	if !strings.Contains(output, "test message") {
		t.Errorf("expected text output with message, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected text output with key=value, got: %s", output)
	}
}

func TestSetupWriter_DebugLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "debug",
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Debug("debug message")

	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Errorf("debug message should be logged at debug level, got: %s", output)
	}
}

func TestSetupWriter_InfoLevelFiltersDebug(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "info",
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Debug("debug message")

	output := buf.String()
	if strings.Contains(output, "debug message") {
		t.Errorf("debug message should NOT be logged at info level, got: %s", output)
	}
}

func TestSetupWriter_WarnLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "warn",
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Info("info message")
	logger.Warn("warn message")

	output := buf.String()
	if strings.Contains(output, "info message") {
		t.Errorf("info message should NOT be logged at warn level")
	}
	if !strings.Contains(output, "warn message") {
		t.Errorf("warn message should be logged at warn level, got: %s", output)
	}
}

func TestSetupWriter_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "error",
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	if strings.Contains(output, "warn message") {
		t.Errorf("warn message should NOT be logged at error level")
	}
	if !strings.Contains(output, "error message") {
		t.Errorf("error message should be logged at error level, got: %s", output)
	}
}

func TestSetupWriter_DefaultsToJSON(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "info",
		Format: "", // Empty should default to JSON
	}

	logger := SetupWriter(cfg, &buf)
	logger.Info("test message")

	output := buf.String()
	var jsonMap map[string]interface{}
	if err := json.Unmarshal([]byte(output), &jsonMap); err != nil {
		t.Errorf("empty format should default to JSON, but got invalid JSON: %v", err)
	}
}

func TestSetupWriter_DefaultsToInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "", // Empty should default to info
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)
	logger.Debug("debug message")
	logger.Info("info message")

	output := buf.String()
	if strings.Contains(output, "debug message") {
		t.Errorf("empty level should default to info, but debug was logged")
	}
	if !strings.Contains(output, "info message") {
		t.Errorf("empty level should default to info, but info was not logged")
	}
}

func TestSetupWriter_CaseInsensitive(t *testing.T) {
	tests := []struct {
		level    string
		expected slog.Level
	}{
		{"DEBUG", slog.LevelDebug},
		{"Debug", slog.LevelDebug},
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"WARN", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"ERROR", slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := config.LogConfig{
				Level:  tt.level,
				Format: "json",
			}

			// Just verify it doesn't panic and creates a logger
			logger := SetupWriter(cfg, &buf)
			if logger == nil {
				t.Error("expected non-nil logger")
			}
		})
	}
}

func TestSetupWriter_SetsDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := config.LogConfig{
		Level:  "info",
		Format: "json",
	}

	logger := SetupWriter(cfg, &buf)

	// The returned logger should be the same one set as default
	if slog.Default() != logger {
		t.Error("expected SetupWriter to set the default logger")
	}
}
