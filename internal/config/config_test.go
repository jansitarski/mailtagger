package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	configYAML := `
llm:
  provider: openai
  model: gpt-4
  api_key: sk-test-key
  temperature: 0.1
  max_tokens: 200
  timeout: 30s

poll_interval: 5m

store:
  type: sqlite
  path: /var/lib/mailtagger/state.db

http:
  addr: :8080
  read_timeout: 10s
  write_timeout: 10s

categories:
  - name: newsletter
    label: automated/newsletter
    description: Marketing emails and newsletters
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.LLM.Provider != "openai" {
		t.Errorf("Expected provider 'openai', got %q", cfg.LLM.Provider)
	}

	if cfg.LLM.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got %q", cfg.LLM.Model)
	}

	if cfg.PollInterval != "5m" {
		t.Errorf("Expected poll_interval '5m', got %q", cfg.PollInterval)
	}

	if len(cfg.Categories) != 1 {
		t.Errorf("Expected 1 category, got %d", len(cfg.Categories))
	}
}

func TestLoad_EnvVarExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "secret-key-123")
	os.Setenv("TEST_DB_PATH", "/tmp/test.db")
	defer os.Unsetenv("TEST_API_KEY")
	defer os.Unsetenv("TEST_DB_PATH")

	configYAML := `
llm:
  provider: openai
  model: gpt-4
  api_key: ${TEST_API_KEY}
  temperature: 0.1
  max_tokens: 200
  timeout: 30s

poll_interval: 5m

store:
  type: sqlite
  path: ${TEST_DB_PATH}

http:
  addr: :8080
  read_timeout: 10s
  write_timeout: 10s

categories:
  - name: newsletter
    label: automated/newsletter
    description: Marketing emails
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.LLM.APIKey != "secret-key-123" {
		t.Errorf("Expected API key 'secret-key-123', got %q", cfg.LLM.APIKey)
	}

	if cfg.Store.Path != "/tmp/test.db" {
		t.Errorf("Expected store path '/tmp/test.db', got %q", cfg.Store.Path)
	}
}

func TestLoad_MissingEnvVar(t *testing.T) {
	configYAML := `
llm:
  provider: openai
  model: gpt-4
  api_key: ${MISSING_VAR}
  temperature: 0.1
  max_tokens: 200
  timeout: 30s

store:
  type: sqlite
  path: /tmp/test.db

http:
  addr: :8080
  read_timeout: 10s
  write_timeout: 10s

categories:
  - name: newsletter
    label: automated/newsletter
    description: Marketing emails
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Missing env vars should remain as ${VAR}
	if cfg.LLM.APIKey != "${MISSING_VAR}" {
		t.Errorf("Expected API key '${MISSING_VAR}', got %q", cfg.LLM.APIKey)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4",
			APIKey:      "sk-test",
			Temperature: 0.1,
			MaxTokens:   200,
			Timeout:     "30s",
		},
		PollInterval: "5m",
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr:         ":8080",
			ReadTimeout:  "10s",
			WriteTimeout: "10s",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() failed on valid config: %v", err)
	}
}

func TestValidate_MissingProvider(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Model:  "gpt-4",
			APIKey: "sk-test",
		},
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for missing provider, got nil")
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "invalid-provider",
			Model:    "gpt-4",
			APIKey:   "sk-test",
		},
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid provider, got nil")
	}
}

func TestValidate_InvalidTemperature(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4",
			APIKey:      "sk-test",
			Temperature: 1.5, // Invalid: > 1.0
		},
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid temperature, got nil")
	}
}

func TestValidate_NoCategories(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
			APIKey:   "sk-test",
		},
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{}, // Empty
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for no categories, got nil")
	}
}

func TestValidate_InvalidDuration(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
			APIKey:   "sk-test",
			Timeout:  "invalid-duration",
		},
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid duration, got nil")
	}
}

func TestValidate_InvalidPollInterval(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
			APIKey:   "sk-test",
		},
		PollInterval: "invalid",
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid poll_interval, got nil")
	}
}

func TestValidate_OllamaNoAPIKey(t *testing.T) {
	// Ollama should not require an API key
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "ollama",
			Model:    "llama2",
			// APIKey is intentionally empty
		},
		Store: StoreConfig{
			Type: "sqlite",
			Path: "/tmp/state.db",
		},
		HTTP: HTTPConfig{
			Addr: ":8080",
		},
		Categories: []Category{
			{
				Name:        "newsletter",
				Label:       "automated/newsletter",
				Description: "Marketing emails",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() failed for ollama without API key: %v", err)
	}
}
