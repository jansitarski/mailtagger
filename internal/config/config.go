package config

import "time"

// Config is the top-level configuration structure for mailtagger.
type Config struct {
	LLM              LLMConfig    `yaml:"llm"`
	Store            StoreConfig  `yaml:"store"`
	HTTP             HTTPConfig   `yaml:"http"`
	Log              LogConfig    `yaml:"log"`
	Admin            AdminConfig  `yaml:"admin"`
	Categories       []Category   `yaml:"categories"`
	PollInterval     string       `yaml:"poll_interval"`         // e.g., "5m", parsed as time.Duration
	MaxMessagesPerTick *int       `yaml:"max_messages_per_tick"` // max messages to process per tick (nil = use default, 0 = unlimited)
	IncludeBody      bool         `yaml:"include_body"`          // opt-in: also send a trimmed message body to the LLM (default false = sender + subject only; body is otherwise never fetched)
	ClientSecretPath string       `yaml:"client_secret_path"`    // path to OAuth client_secret.json file
	EncryptionKey    string       `yaml:"encryption_key"`        // 32-byte encryption key in hex (or use MAILTAGGER_ENCRYPTION_KEY env var)
}

// LLMConfig defines the LLM provider configuration.
type LLMConfig struct {
	Provider     string  `yaml:"provider"`      // "openai", "anthropic", "gemini", "ollama"
	Model        string  `yaml:"model"`         // e.g., "gpt-4", "claude-3-5-sonnet-20241022"
	APIKey       string  `yaml:"api_key"`       // supports ${ENV} expansion
	BaseURL      string  `yaml:"base_url"`      // optional, for custom endpoints
	Temperature  float64 `yaml:"temperature"`   // 0.0-1.0
	MaxTokens    int     `yaml:"max_tokens"`    // max response tokens
	Timeout      string  `yaml:"timeout"`       // e.g., "30s", parsed as time.Duration
	SystemPrompt string  `yaml:"system_prompt"` // optional, custom system prompt template
}

// StoreConfig defines the state storage backend.
type StoreConfig struct {
	Type string `yaml:"type"` // "sqlite" (default), "memory"
	Path string `yaml:"path"` // path to SQLite database file
}

// HTTPConfig defines the HTTP server settings.
type HTTPConfig struct {
	Addr           string `yaml:"addr"`            // listen address, e.g., ":8080"
	ReadTimeout    string `yaml:"read_timeout"`    // e.g., "10s", parsed as time.Duration
	WriteTimeout   string `yaml:"write_timeout"`   // e.g., "10s", parsed as time.Duration
	MetricsEnabled bool   `yaml:"metrics_enabled"` // enable /metrics endpoint
}

// LogConfig defines logging settings.
type LogConfig struct {
	Level  string `yaml:"level"`  // "debug", "info", "warn", "error" (default: "info")
	Format string `yaml:"format"` // "json" or "text" (default: "json")
}

// AdminConfig defines admin dashboard settings.
type AdminConfig struct {
	Enabled  *bool  `yaml:"enabled"`  // nil = enabled by default; set to false to disable the /admin dashboard
	Password string `yaml:"password"` // optional basic auth password (username is always "admin")
}

// Category defines a classification category and its Gmail label.
type Category struct {
	Name        string `yaml:"name"`        // unique category name
	Label       string `yaml:"label"`       // Gmail label to apply
	Description string `yaml:"description"` // description for the LLM classifier
}

// ParsedDurations provides parsed time.Duration values for string fields.
func (c *Config) ParsedDurations() (map[string]time.Duration, error) {
	durations := make(map[string]time.Duration)

	if c.LLM.Timeout != "" {
		d, err := time.ParseDuration(c.LLM.Timeout)
		if err != nil {
			return nil, err
		}
		durations["llm.timeout"] = d
	}

	if c.PollInterval != "" {
		d, err := time.ParseDuration(c.PollInterval)
		if err != nil {
			return nil, err
		}
		durations["poll_interval"] = d
	}

	if c.HTTP.ReadTimeout != "" {
		d, err := time.ParseDuration(c.HTTP.ReadTimeout)
		if err != nil {
			return nil, err
		}
		durations["http.read_timeout"] = d
	}

	if c.HTTP.WriteTimeout != "" {
		d, err := time.ParseDuration(c.HTTP.WriteTimeout)
		if err != nil {
			return nil, err
		}
		durations["http.write_timeout"] = d
	}

	return durations, nil
}
