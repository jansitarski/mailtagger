package gmail

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// HistoryResult contains the results of a history sync operation.
type HistoryResult struct {
	// Messages contains the message IDs that were added since the last sync
	MessageIDs []string
	// NextHistoryID is the history ID to use for the next sync
	NextHistoryID string
	// Bootstrapped indicates if this was a bootstrap operation (history was invalid)
	Bootstrapped bool
}

// SyncHistory fetches message additions since the given historyID.
// Returns message IDs added since the historyID and the new historyID for the next sync.
// If the history ID is invalid (404), it automatically falls back to bootstrapping.
// If historyID is empty, this function will return an error indicating a bootstrap is needed.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) SyncHistory(ctx context.Context, startHistoryID string) (*HistoryResult, error) {
	if startHistoryID == "" {
		return nil, fmt.Errorf("startHistoryID is required for history sync")
	}

	// Convert string history ID to uint64
	historyID, err := strconv.ParseUint(startHistoryID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid history ID: %w", err)
	}

	result := &HistoryResult{
		MessageIDs: []string{},
	}

	// Configure the history list request
	req := c.service.Users.History.List("me").
		Context(ctx).
		StartHistoryId(historyID).
		HistoryTypes("messageAdded")

	// Execute with rate limiting - Pages handles pagination internally
	// We need to wrap the entire pagination operation
	err = c.rateLimiter.Do(ctx, func() error {
		return req.Pages(ctx, func(resp *gmail.ListHistoryResponse) error {
			// Extract message IDs from history records
			for _, history := range resp.History {
				for _, msg := range history.MessagesAdded {
					if msg.Message != nil && msg.Message.Id != "" {
						result.MessageIDs = append(result.MessageIDs, msg.Message.Id)
					}
				}
			}

			// Update the next history ID
			if resp.HistoryId != 0 {
				result.NextHistoryID = fmt.Sprintf("%d", resp.HistoryId)
			}

			return nil
		})
	})

	if err != nil {
		// Check if this is a 404 error (history ID no longer valid)
		if apiErr, ok := err.(*googleapi.Error); ok && apiErr.Code == 404 {
			// History ID is invalid, fall back to bootstrap
			return c.Bootstrap(ctx)
		}
		return nil, fmt.Errorf("failed to list history: %w", err)
	}

	// If we didn't get a history ID from the response, use the start ID
	if result.NextHistoryID == "" {
		result.NextHistoryID = startHistoryID
	}

	return result, nil
}

// GetCurrentHistoryID fetches the current history ID for the mailbox.
// This is useful for bootstrapping when you don't have a history ID yet.
// It returns the current history ID without fetching any messages.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) GetCurrentHistoryID(ctx context.Context) (string, error) {
	var profile *gmail.Profile

	// Execute with rate limiting and retry
	err := c.rateLimiter.Do(ctx, func() error {
		var apiErr error
		profile, apiErr = c.service.Users.GetProfile("me").Context(ctx).Do()
		return apiErr
	})

	if err != nil {
		return "", fmt.Errorf("failed to get profile: %w", err)
	}

	if profile.HistoryId == 0 {
		return "", fmt.Errorf("no history ID available")
	}

	return fmt.Sprintf("%d", profile.HistoryId), nil
}

// Bootstrap performs a full mailbox scan to get all message IDs and the current history ID.
// This is used when there's no history ID or when the history ID is no longer valid (404).
// Note: This can be expensive for large mailboxes. Consider using query filters.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) Bootstrap(ctx context.Context) (*HistoryResult, error) {
	result := &HistoryResult{
		MessageIDs:   []string{},
		Bootstrapped: true,
	}

	// List all messages with rate limiting
	req := c.service.Users.Messages.List("me").Context(ctx)

	// Execute with rate limiting - wrap the entire pagination operation
	err := c.rateLimiter.Do(ctx, func() error {
		return req.Pages(ctx, func(resp *gmail.ListMessagesResponse) error {
			for _, msg := range resp.Messages {
				if msg.Id != "" {
					result.MessageIDs = append(result.MessageIDs, msg.Id)
				}
			}
			return nil
		})
	})

	if err != nil {
		return nil, fmt.Errorf("failed to bootstrap messages: %w", err)
	}

	// Get the current history ID for future syncs
	historyID, err := c.GetCurrentHistoryID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get history ID after bootstrap: %w", err)
	}
	result.NextHistoryID = historyID

	return result, nil
}
