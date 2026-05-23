package config

import "time"

// Config is the top-level configuration structure for mailtagger.
type Config struct {
	LLM       LLMConfig       `yaml:"llm"`
	Accounts  []AccountConfig `yaml:"accounts"`
	Store     StoreConfig     `yaml:"store"`
	HTTP      HTTPConfig      `yaml:"http"`
	Categories []Category     `yaml:"categories"`
}

// LLMConfig defines the LLM provider configuration.
type LLMConfig struct {
	Provider    string  `yaml:"provider"`     // "openai", "anthropic", "gemini", "ollama"
	Model       string  `yaml:"model"`        // e.g., "gpt-4", "claude-3-5-sonnet-20241022"
	APIKey      string  `yaml:"api_key"`      // supports ${ENV} expansion
	BaseURL     string  `yaml:"base_url"`     // optional, for custom endpoints
	Temperature float64 `yaml:"temperature"`  // 0.0-1.0
	MaxTokens   int     `yaml:"max_tokens"`   // max response tokens
	Timeout     string  `yaml:"timeout"`      // e.g., "30s", parsed as time.Duration
}

// AccountConfig defines a Gmail account to monitor.
type AccountConfig struct {
	ID               string `yaml:"id"`                  // unique account identifier
	Email            string `yaml:"email"`               // Gmail address
	ClientSecretPath string `yaml:"client_secret_path"`  // path to OAuth client_secret.json
	TokenPath        string `yaml:"token_path"`          // path to store OAuth tokens
	PollInterval     string `yaml:"poll_interval"`       // e.g., "5m", parsed as time.Duration
	Query            string `yaml:"query"`               // Gmail search query filter
}

// StoreConfig defines the state storage backend.
type StoreConfig struct {
	Type string `yaml:"type"` // "sqlite" (default), "memory"
	Path string `yaml:"path"` // path to SQLite database file
}

// HTTPConfig defines the HTTP server settings.
type HTTPConfig struct {
	Addr         string `yaml:"addr"`           // listen address, e.g., ":8080"
	ReadTimeout  string `yaml:"read_timeout"`   // e.g., "10s", parsed as time.Duration
	WriteTimeout string `yaml:"write_timeout"`  // e.g., "10s", parsed as time.Duration
}

// Category defines a classification category and its Gmail label.
type Category struct {
	Name        string `yaml:"name"`         // unique category name
	Label       string `yaml:"label"`        // Gmail label to apply
	Description string `yaml:"description"`  // description for the LLM classifier
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
	
	for _, acc := range c.Accounts {
		if acc.PollInterval != "" {
			d, err := time.ParseDuration(acc.PollInterval)
			if err != nil {
				return nil, err
			}
			durations["accounts["+acc.ID+"].poll_interval"] = d
		}
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
