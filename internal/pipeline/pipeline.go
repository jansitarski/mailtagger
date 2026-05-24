// Package pipeline provides the email classification pipeline orchestrator.
package pipeline

import (
	"context"
	"time"

	"github.com/jansitarski/mailtagger/internal/classifier"
	"github.com/jansitarski/mailtagger/internal/config"
	"github.com/jansitarski/mailtagger/internal/gmail"
	"github.com/jansitarski/mailtagger/internal/store"
)

// DefaultPollInterval is the default polling interval if not configured.
const DefaultPollInterval = 5 * time.Minute

// GmailClientFactory creates Gmail clients for accounts.
type GmailClientFactory interface {
	// NewClient creates a Gmail client for the given account.
	NewClient(ctx context.Context, account *store.Account) (*gmail.Client, error)
}

// Pipeline orchestrates email classification for all configured accounts.
// It polls Gmail for new messages, classifies them using the LLM, and applies labels.
type Pipeline struct {
	store      *store.Store
	classifier *classifier.Classifier
	gmailFactory GmailClientFactory
	config     *config.Config
	categories map[string]string // category name -> label name mapping
}

// New creates a new Pipeline with the given dependencies.
func New(
	st *store.Store,
	cls *classifier.Classifier,
	gmailFactory GmailClientFactory,
	cfg *config.Config,
) *Pipeline {
	// Build category -> label mapping
	categories := make(map[string]string)
	for _, cat := range cfg.Categories {
		categories[cat.Name] = cat.Label
	}

	return &Pipeline{
		store:        st,
		classifier:   cls,
		gmailFactory: gmailFactory,
		config:       cfg,
		categories:   categories,
	}
}

// Store returns the store instance.
func (p *Pipeline) Store() *store.Store {
	return p.store
}

// Classifier returns the classifier instance.
func (p *Pipeline) Classifier() *classifier.Classifier {
	return p.classifier
}

// Config returns the config instance.
func (p *Pipeline) Config() *config.Config {
	return p.config
}

// GetLabelForCategory returns the Gmail label for a category.
// Returns empty string if category is not found.
func (p *Pipeline) GetLabelForCategory(category string) string {
	return p.categories[category]
}

// Run starts the pipeline tick loop. It polls for new messages at the configured
// interval and processes them. The loop runs until the context is cancelled.
// Returns nil when stopped via context cancellation.
func (p *Pipeline) Run(ctx context.Context) error {
	// Determine poll interval from first account's config, or use default
	pollInterval := DefaultPollInterval
	if len(p.config.Accounts) > 0 && p.config.Accounts[0].PollInterval != "" {
		if d, err := time.ParseDuration(p.config.Accounts[0].PollInterval); err == nil {
			pollInterval = d
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Run once immediately on startup
	if err := p.tick(ctx); err != nil {
		// Log error but continue - don't fail on first tick error
		_ = err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.tick(ctx); err != nil {
				// Log error but continue polling
				_ = err
			}
		}
	}
}

// tick performs a single polling cycle for all accounts.
// It fetches new messages, classifies them, and applies labels.
func (p *Pipeline) tick(ctx context.Context) error {
	// Get all accounts from store
	accounts, err := p.store.ListAccounts()
	if err != nil {
		return err
	}

	// Process each account
	for _, account := range accounts {
		if err := p.processAccount(ctx, account); err != nil {
			// Log error but continue with other accounts
			_ = err
			continue
		}
	}

	return nil
}

// processAccount processes a single account during a tick cycle.
// This is a placeholder that will be implemented in task 3.
func (p *Pipeline) processAccount(ctx context.Context, account *store.Account) error {
	// Will be implemented in task mailtagger-6zk.3
	return nil
}
