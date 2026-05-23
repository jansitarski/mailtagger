package gmail

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/api/gmail/v1"
)

// HistoryResult contains the results of a history sync operation.
type HistoryResult struct {
	// Messages contains the message IDs that were added since the last sync
	MessageIDs []string
	// NextHistoryID is the history ID to use for the next sync
	NextHistoryID string
	// HasMore indicates if there are more history records to fetch
	HasMore bool
}

// SyncHistory fetches message additions since the given historyID.
// Returns message IDs added since the historyID and the new historyID for the next sync.
// If historyID is empty, this function will return an error indicating a bootstrap is needed.
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

	// Execute the request with pagination
	err = req.Pages(ctx, func(resp *gmail.ListHistoryResponse) error {
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

	if err != nil {
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
func (c *Client) GetCurrentHistoryID(ctx context.Context) (string, error) {
	// Fetch the user's profile to get the current history ID
	profile, err := c.service.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get profile: %w", err)
	}

	if profile.HistoryId == 0 {
		return "", fmt.Errorf("no history ID available")
	}

	return fmt.Sprintf("%d", profile.HistoryId), nil
}
