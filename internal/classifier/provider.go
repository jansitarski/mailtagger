package classifier

import (
	"context"
	"fmt"
	"strings"

	"github.com/jansitarski/mailtagger/internal/config"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// NewModel creates an llms.Model from the given LLM configuration.
func NewModel(ctx context.Context, cfg config.LLMConfig) (llms.Model, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "openai":
		return newOpenAIModel(cfg)
	case "anthropic":
		return newAnthropicModel(cfg)
	case "gemini":
		return newGeminiModel(ctx, cfg)
	case "ollama":
		return newOllamaModel(cfg)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}
}

func newOpenAIModel(cfg config.LLMConfig) (llms.Model, error) {
	opts := []openai.Option{
		openai.WithModel(cfg.Model),
	}
	if cfg.APIKey != "" {
		opts = append(opts, openai.WithToken(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
	}
	return openai.New(opts...)
}

func newAnthropicModel(cfg config.LLMConfig) (llms.Model, error) {
	opts := []anthropic.Option{
		anthropic.WithModel(cfg.Model),
	}
	if cfg.APIKey != "" {
		opts = append(opts, anthropic.WithToken(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, anthropic.WithBaseURL(cfg.BaseURL))
	}
	return anthropic.New(opts...)
}

func newGeminiModel(ctx context.Context, cfg config.LLMConfig) (llms.Model, error) {
	opts := []googleai.Option{
		googleai.WithDefaultModel(cfg.Model),
	}
	if cfg.APIKey != "" {
		opts = append(opts, googleai.WithAPIKey(cfg.APIKey))
	}
	return googleai.New(ctx, opts...)
}

func newOllamaModel(cfg config.LLMConfig) (llms.Model, error) {
	opts := []ollama.Option{
		ollama.WithModel(cfg.Model),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, ollama.WithServerURL(cfg.BaseURL))
	}
	return ollama.New(opts...)
}
