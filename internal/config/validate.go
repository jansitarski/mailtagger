package config

import (
	"fmt"
	"strings"
	"time"
)

// Validate checks the configuration for required fields and valid values.
func (c *Config) Validate() error {
	// Validate LLM config
	if err := c.validateLLM(); err != nil {
		return fmt.Errorf("llm config: %w", err)
	}

	// Validate store config
	if err := c.validateStore(); err != nil {
		return fmt.Errorf("store config: %w", err)
	}

	// Validate HTTP config
	if err := c.validateHTTP(); err != nil {
		return fmt.Errorf("http config: %w", err)
	}

	// Validate poll interval if set
	if c.PollInterval != "" {
		d, err := time.ParseDuration(c.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid poll_interval duration %q: %w", c.PollInterval, err)
		}
		if d <= 0 {
			return fmt.Errorf("poll_interval must be positive, got %s", c.PollInterval)
		}
	}

	// Validate categories
	if len(c.Categories) == 0 {
		return fmt.Errorf("at least one category must be configured")
	}
	for i, cat := range c.Categories {
		if err := cat.validate(); err != nil {
			return fmt.Errorf("category[%d] (%s): %w", i, cat.Name, err)
		}
	}

	return nil
}

func (c *Config) validateLLM() error {
	if c.LLM.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	validProviders := map[string]bool{
		"openai":    true,
		"anthropic": true,
		"gemini":    true,
		"ollama":    true,
	}
	if !validProviders[strings.ToLower(c.LLM.Provider)] {
		return fmt.Errorf("invalid provider %q, must be one of: openai, anthropic, gemini, ollama", c.LLM.Provider)
	}

	if c.LLM.Model == "" {
		return fmt.Errorf("model is required")
	}

	// API key is required for all providers except ollama
	if strings.ToLower(c.LLM.Provider) != "ollama" && c.LLM.APIKey == "" {
		return fmt.Errorf("api_key is required for provider %q", c.LLM.Provider)
	}

	if c.LLM.Temperature < 0 || c.LLM.Temperature > 1 {
		return fmt.Errorf("temperature must be between 0.0 and 1.0, got %.2f", c.LLM.Temperature)
	}

	if c.LLM.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be non-negative, got %d", c.LLM.MaxTokens)
	}

	if c.LLM.Timeout != "" {
		if _, err := time.ParseDuration(c.LLM.Timeout); err != nil {
			return fmt.Errorf("invalid timeout duration %q: %w", c.LLM.Timeout, err)
		}
	}

	return nil
}

func (c *Config) validateStore() error {
	if c.Store.Type == "" {
		c.Store.Type = "sqlite" // default
	}

	validTypes := map[string]bool{
		"sqlite": true,
		"memory": true,
	}
	if !validTypes[strings.ToLower(c.Store.Type)] {
		return fmt.Errorf("invalid type %q, must be one of: sqlite, memory", c.Store.Type)
	}

	if strings.ToLower(c.Store.Type) == "sqlite" && c.Store.Path == "" {
		return fmt.Errorf("path is required for sqlite store")
	}

	return nil
}

func (c *Config) validateHTTP() error {
	if c.HTTP.Addr == "" {
		return fmt.Errorf("addr is required")
	}

	if c.HTTP.ReadTimeout != "" {
		if _, err := time.ParseDuration(c.HTTP.ReadTimeout); err != nil {
			return fmt.Errorf("invalid read_timeout duration %q: %w", c.HTTP.ReadTimeout, err)
		}
	}

	if c.HTTP.WriteTimeout != "" {
		if _, err := time.ParseDuration(c.HTTP.WriteTimeout); err != nil {
			return fmt.Errorf("invalid write_timeout duration %q: %w", c.HTTP.WriteTimeout, err)
		}
	}

	return nil
}

func (cat *Category) validate() error {
	if cat.Name == "" {
		return fmt.Errorf("name is required")
	}

	if cat.Label == "" {
		return fmt.Errorf("label is required")
	}

	if cat.Description == "" {
		return fmt.Errorf("description is required")
	}

	return nil
}
