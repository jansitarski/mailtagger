package gmail

import (
	"context"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

// LabelCache defines the interface for caching Gmail labels.
type LabelCache interface {
	// GetLabel retrieves a label by account ID and label name.
	GetLabel(accountID int64, labelName string) (labelID string, err error)
	// UpsertLabel inserts or updates a label mapping.
	UpsertLabel(accountID int64, labelName, labelID string) error
	// ListLabels returns all cached labels for an account.
	ListLabels(accountID int64) (labels []CachedLabel, err error)
}

// CachedLabel represents a cached Gmail label.
type CachedLabel struct {
	Name string
	ID   string
}

// LabelManager handles Gmail label operations with caching.
type LabelManager struct {
	client    *Client
	cache     LabelCache
	accountID int64
}

// NewLabelManager creates a new label manager.
func NewLabelManager(client *Client, cache LabelCache, accountID int64) *LabelManager {
	return &LabelManager{
		client:    client,
		cache:     cache,
		accountID: accountID,
	}
}

// SyncLabels fetches all labels from Gmail and updates the cache.
// This should be called on startup to ensure the cache is up-to-date.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (lm *LabelManager) SyncLabels(ctx context.Context) error {
	var resp *gmail.ListLabelsResponse

	// Execute with rate limiting and retry
	err := lm.client.rateLimiter.Do(ctx, func() error {
		var apiErr error
		resp, apiErr = lm.client.service.Users.Labels.List("me").Context(ctx).Do()
		return apiErr
	})

	if err != nil {
		return fmt.Errorf("failed to list labels: %w", err)
	}

	// Update the cache with each label
	for _, label := range resp.Labels {
		if err := lm.cache.UpsertLabel(lm.accountID, label.Name, label.Id); err != nil {
			return fmt.Errorf("failed to cache label %s: %w", label.Name, err)
		}
	}

	return nil
}

// GetLabelID retrieves a label ID by name from the cache.
// Returns an error if the label is not found in the cache.
func (lm *LabelManager) GetLabelID(ctx context.Context, labelName string) (string, error) {
	labelID, err := lm.cache.GetLabel(lm.accountID, labelName)
	if err != nil {
		return "", fmt.Errorf("label %s not found in cache: %w", labelName, err)
	}
	return labelID, nil
}

// ListLabels returns all cached labels for the account.
func (lm *LabelManager) ListLabels(ctx context.Context) ([]CachedLabel, error) {
	return lm.cache.ListLabels(lm.accountID)
}

// CreateLabel creates a new label in Gmail and caches it.
// Returns the label ID.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (lm *LabelManager) CreateLabel(ctx context.Context, labelName string) (string, error) {
	// Create the label in Gmail
	label := &gmail.Label{
		Name:                   labelName,
		LabelListVisibility:    "labelShow",
		MessageListVisibility:  "show",
	}

	var created *gmail.Label

	// Execute with rate limiting and retry
	err := lm.client.rateLimiter.Do(ctx, func() error {
		var apiErr error
		created, apiErr = lm.client.service.Users.Labels.Create("me", label).Context(ctx).Do()
		return apiErr
	})

	if err != nil {
		return "", fmt.Errorf("failed to create label %s: %w", labelName, err)
	}

	// Cache the new label
	if err := lm.cache.UpsertLabel(lm.accountID, created.Name, created.Id); err != nil {
		return "", fmt.Errorf("failed to cache created label %s: %w", labelName, err)
	}

	return created.Id, nil
}

// GetOrCreateLabel retrieves a label ID by name, creating it if it doesn't exist.
// This implements lazy label creation.
func (lm *LabelManager) GetOrCreateLabel(ctx context.Context, labelName string) (string, error) {
	// Try to get from cache first
	labelID, err := lm.GetLabelID(ctx, labelName)
	if err == nil {
		return labelID, nil
	}

	// Label not in cache, try to create it
	return lm.CreateLabel(ctx, labelName)
}
