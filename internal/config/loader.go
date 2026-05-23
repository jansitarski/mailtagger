package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load reads and parses a YAML configuration file from the given path.
// Returns a Config struct or an error if the file cannot be read or parsed.
// Expands ${ENV} variables in string fields after parsing.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Expand environment variables in config values
	expandEnvVars(&cfg)

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// expandEnvVars replaces ${ENV_VAR} patterns with their environment variable values.
func expandEnvVars(cfg *Config) {
	// LLM config
	cfg.LLM.APIKey = expandString(cfg.LLM.APIKey)
	cfg.LLM.BaseURL = expandString(cfg.LLM.BaseURL)

	// Account configs
	for i := range cfg.Accounts {
		cfg.Accounts[i].ClientSecretPath = expandString(cfg.Accounts[i].ClientSecretPath)
		cfg.Accounts[i].TokenPath = expandString(cfg.Accounts[i].TokenPath)
		cfg.Accounts[i].Query = expandString(cfg.Accounts[i].Query)
	}

	// Store config
	cfg.Store.Path = expandString(cfg.Store.Path)
}

// expandString replaces ${VAR} with the value of the environment variable VAR.
func expandString(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Extract the variable name from ${VAR}
		varName := envVarPattern.FindStringSubmatch(match)[1]
		value := os.Getenv(varName)
		if value == "" {
			// If not set, return the original ${VAR} pattern
			return match
		}
		return value
	})
}
