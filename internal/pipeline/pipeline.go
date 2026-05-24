// Package pipeline provides the email classification pipeline orchestrator.
package pipeline

import (
	"context"

	"github.com/jansitarski/mailtagger/internal/classifier"
	"github.com/jansitarski/mailtagger/internal/config"
	"github.com/jansitarski/mailtagger/internal/gmail"
	"github.com/jansitarski/mailtagger/internal/store"
)

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
