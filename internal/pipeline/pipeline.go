// Package pipeline provides the email classification pipeline orchestrator.
package pipeline

import (
	"context"
	"strings"
	"time"

	"github.com/jansitarski/mailtagger/internal/classifier"
	"github.com/jansitarski/mailtagger/internal/config"
	"github.com/jansitarski/mailtagger/internal/gmail"
	"github.com/jansitarski/mailtagger/internal/store"
)

// DefaultPollInterval is the default polling interval if not configured.
const DefaultPollInterval = 5 * time.Minute

// AILabelPrefix is the prefix for AI-generated labels.
const AILabelPrefix = "AI/"

// DefaultMaxMessagesPerTick is the default max messages per tick if not configured.
// 0 means unlimited.
const DefaultMaxMessagesPerTick = 50

// GmailClientFactory creates Gmail clients for accounts.
type GmailClientFactory interface {
	// NewClient creates a Gmail client for the given account.
	NewClient(ctx context.Context, account *store.Account) (*gmail.Client, error)
}

// Pipeline orchestrates email classification for all configured accounts.
// It polls Gmail for new messages, classifies them using the LLM, and applies labels.
type Pipeline struct {
	store        *store.Store
	classifier   *classifier.Classifier
	gmailFactory GmailClientFactory
	config       *config.Config
	categories   map[string]string // category name -> label name mapping
	aiLabelIDs   map[int64]map[string]bool // account ID -> set of AI label IDs
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
		aiLabelIDs:   make(map[int64]map[string]bool),
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
// It fetches history to get new message IDs and processes them.
func (p *Pipeline) processAccount(ctx context.Context, account *store.Account) error {
	// Create Gmail client for this account
	client, err := p.gmailFactory.NewClient(ctx, account)
	if err != nil {
		return err
	}

	// Fetch new message IDs using history sync
	messageIDs, newHistoryID, err := p.fetchNewMessageIDs(ctx, client, account)
	if err != nil {
		return err
	}

	// Apply max messages per tick throttle
	maxMessages := p.config.MaxMessagesPerTick
	if maxMessages == 0 {
		maxMessages = DefaultMaxMessagesPerTick
	}
	if maxMessages > 0 && len(messageIDs) > maxMessages {
		messageIDs = messageIDs[:maxMessages]
	}

	// Track how many messages were processed for this tick
	processed := 0
	for _, msgID := range messageIDs {
		if err := p.processMessage(ctx, client, account, msgID); err != nil {
			// Log error but continue with other messages
			_ = err
			continue
		}
		processed++
	}

	// Update history ID if we got new messages
	if newHistoryID != "" && newHistoryID != account.HistoryID {
		if err := p.store.UpdateHistoryID(account.ID, newHistoryID); err != nil {
			return err
		}
	}

	return nil
}

// fetchNewMessageIDs fetches new message IDs for an account using history sync.
// If no history ID exists, it bootstraps by getting the current history ID.
func (p *Pipeline) fetchNewMessageIDs(ctx context.Context, client *gmail.Client, account *store.Account) ([]string, string, error) {
	// If no history ID, bootstrap to get the current one
	if account.HistoryID == "" {
		historyID, err := client.GetCurrentHistoryID(ctx)
		if err != nil {
			return nil, "", err
		}
		// Return empty messages - we'll start processing from next tick
		return nil, historyID, nil
	}

	// Sync history to get new messages
	result, err := client.SyncHistory(ctx, account.HistoryID)
	if err != nil {
		return nil, "", err
	}

	return result.MessageIDs, result.NextHistoryID, nil
}

// processMessage processes a single message: fetch -> classify -> label -> record.
func (p *Pipeline) processMessage(ctx context.Context, client *gmail.Client, account *store.Account, messageID string) error {
	// Check if message was already processed
	skip, err := p.shouldSkipMessage(ctx, client, account, messageID)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}

	// 1. Fetch the full message
	msg, err := client.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}

	// 2. Extract the body for classification
	body, err := gmail.ExtractBody(msg.RawMessage)
	if err != nil {
		// Use snippet as fallback
		body = msg.Snippet
	}
	
	// Clean the body (strip quoted replies, truncate)
	body = gmail.CleanBody(body, 4000) // 4k chars max for LLM

	// 3. Classify the email
	email := classifier.Email{
		ID:      messageID,
		From:    msg.From,
		Subject: msg.Subject,
		Body:    body,
	}

	decision, err := p.classifier.Classify(ctx, email)
	if err != nil {
		return err
	}

	// 4. Get the label for the category
	labelName := p.GetLabelForCategory(decision.Category)
	if labelName == "" {
		// No label configured for this category, skip labeling
		// but still record as processed
		if err := p.store.InsertProcessedMessage(account.ID, messageID); err != nil {
			return err
		}
		return nil
	}

	// 5. Get or create the Gmail label
	labelManager := gmail.NewLabelManager(client, &storeLabelCache{store: p.store}, account.ID)
	labelID, err := labelManager.GetOrCreateLabel(ctx, labelName)
	if err != nil {
		return err
	}

	// 6. Apply the label to the message
	if err := client.AddLabels(ctx, messageID, []string{labelID}); err != nil {
		return err
	}

	// 7. Record the message as processed
	if err := p.store.InsertProcessedMessage(account.ID, messageID); err != nil {
		return err
	}

	return nil
}

// storeLabelCache adapts store.Store to gmail.LabelCache interface.
type storeLabelCache struct {
	store *store.Store
}

func (c *storeLabelCache) GetLabel(accountID int64, labelName string) (string, error) {
	label, err := c.store.GetLabel(accountID, labelName)
	if err != nil {
		return "", err
	}
	return label.LabelID, nil
}

func (c *storeLabelCache) UpsertLabel(accountID int64, labelName, labelID string) error {
	return c.store.UpsertLabel(accountID, labelName, labelID)
}

func (c *storeLabelCache) ListLabels(accountID int64) ([]gmail.CachedLabel, error) {
	labels, err := c.store.ListLabels(accountID)
	if err != nil {
		return nil, err
	}
	result := make([]gmail.CachedLabel, len(labels))
	for i, l := range labels {
		result[i] = gmail.CachedLabel{
			Name: l.LabelName,
			ID:   l.LabelID,
		}
	}
	return result, nil
}

// shouldSkipMessage checks if a message should be skipped.
// Returns true if:
// - Message is already in processed_messages table
// - Message already has an AI/* label
func (p *Pipeline) shouldSkipMessage(ctx context.Context, client *gmail.Client, account *store.Account, messageID string) (bool, error) {
	// Check if already processed in our store
	exists, err := p.store.ProcessedMessageExists(account.ID, messageID)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}

	// Check if message already has an AI/* label
	msg, err := client.GetMessage(ctx, messageID)
	if err != nil {
		return false, err
	}

	if p.hasAILabel(account.ID, msg) {
		return true, nil
	}

	return false, nil
}

// hasAILabel checks if a message already has an AI/* label.
// It checks the message's label IDs against the cached set of AI label IDs for the account.
func (p *Pipeline) hasAILabel(accountID int64, msg *gmail.Message) bool {
	aiLabels, ok := p.aiLabelIDs[accountID]
	if !ok || len(aiLabels) == 0 {
		return false
	}

	for _, labelID := range msg.LabelIDs {
		if aiLabels[labelID] {
			return true
		}
	}

	return false
}

// cacheAILabelIDs caches the label IDs for AI/* labels for an account.
// This should be called during account initialization to build the cache.
func (p *Pipeline) cacheAILabelIDs(accountID int64, labelIDs map[string]string) {
	aiLabels := make(map[string]bool)
	for labelName, labelID := range labelIDs {
		if strings.HasPrefix(labelName, AILabelPrefix) {
			aiLabels[labelID] = true
		}
	}
	p.aiLabelIDs[accountID] = aiLabels
}
