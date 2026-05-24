package gmail

import (
	"context"
	"fmt"

	"google.golang.org/api/gmail/v1"
)

// Message represents a Gmail message with parsed metadata and body.
type Message struct {
	ID          string
	ThreadID    string
	LabelIDs    []string
	Snippet     string
	HistoryID   string
	InternalDate int64
	From        string
	To          string
	Subject     string
	Date        string
	RawMessage  *gmail.Message // Full raw message for advanced processing
}

// GetMessage fetches a message by ID with format=full.
// Returns the full message including headers and body parts.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	var gmailMsg *gmail.Message

	// Execute with rate limiting and retry
	err := c.rateLimiter.Do(ctx, func() error {
		var apiErr error
		gmailMsg, apiErr = c.service.Users.Messages.Get("me", messageID).
			Context(ctx).
			Format("full").
			Do()
		return apiErr
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get message %s: %w", messageID, err)
	}

	// Parse the message into our structure
	msg := &Message{
		ID:           gmailMsg.Id,
		ThreadID:     gmailMsg.ThreadId,
		LabelIDs:     gmailMsg.LabelIds,
		Snippet:      gmailMsg.Snippet,
		HistoryID:    fmt.Sprintf("%d", gmailMsg.HistoryId),
		InternalDate: gmailMsg.InternalDate,
		RawMessage:   gmailMsg,
	}

	// Extract common headers
	if gmailMsg.Payload != nil {
		for _, header := range gmailMsg.Payload.Headers {
			switch header.Name {
			case "From":
				msg.From = header.Value
			case "To":
				msg.To = header.Value
			case "Subject":
				msg.Subject = header.Value
			case "Date":
				msg.Date = header.Value
			}
		}
	}

	return msg, nil
}

// GetMessages fetches multiple messages by their IDs.
// Returns a map of message ID to Message. Fails fast on the first error.
func (c *Client) GetMessages(ctx context.Context, messageIDs []string) (map[string]*Message, error) {
	messages := make(map[string]*Message)
	
	for _, msgID := range messageIDs {
		msg, err := c.GetMessage(ctx, msgID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch message %s: %w", msgID, err)
		}
		messages[msgID] = msg
	}

	return messages, nil
}

// AddLabels adds one or more labels to a message.
// Returns the updated message with new label IDs.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) AddLabels(ctx context.Context, messageID string, labelIDs []string) error {
	if messageID == "" {
		return fmt.Errorf("message ID is required")
	}
	if len(labelIDs) == 0 {
		return fmt.Errorf("at least one label ID is required")
	}

	// Create the modification request
	req := &gmail.ModifyMessageRequest{
		AddLabelIds: labelIDs,
	}

	// Execute with rate limiting and retry - return raw API error for retry detection
	err := c.rateLimiter.Do(ctx, func() error {
		_, apiErr := c.service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do()
		return apiErr
	})
	if err != nil {
		return fmt.Errorf("failed to add labels to message %s: %w", messageID, err)
	}
	return nil
}

// RemoveLabels removes one or more labels from a message.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) RemoveLabels(ctx context.Context, messageID string, labelIDs []string) error {
	if messageID == "" {
		return fmt.Errorf("message ID is required")
	}
	if len(labelIDs) == 0 {
		return fmt.Errorf("at least one label ID is required")
	}

	// Create the modification request
	req := &gmail.ModifyMessageRequest{
		RemoveLabelIds: labelIDs,
	}

	// Execute with rate limiting and retry - return raw API error for retry detection
	err := c.rateLimiter.Do(ctx, func() error {
		_, apiErr := c.service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do()
		return apiErr
	})
	if err != nil {
		return fmt.Errorf("failed to remove labels from message %s: %w", messageID, err)
	}
	return nil
}

// ModifyLabels adds and/or removes labels from a message in a single operation.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) ModifyLabels(ctx context.Context, messageID string, addLabelIDs, removeLabelIDs []string) error {
	if messageID == "" {
		return fmt.Errorf("message ID is required")
	}
	if len(addLabelIDs) == 0 && len(removeLabelIDs) == 0 {
		return fmt.Errorf("at least one label ID to add or remove is required")
	}

	// Create the modification request
	req := &gmail.ModifyMessageRequest{
		AddLabelIds:    addLabelIDs,
		RemoveLabelIds: removeLabelIDs,
	}

	// Execute with rate limiting and retry - return raw API error for retry detection
	err := c.rateLimiter.Do(ctx, func() error {
		_, apiErr := c.service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do()
		return apiErr
	})
	if err != nil {
		return fmt.Errorf("failed to modify labels for message %s: %w", messageID, err)
	}
	return nil
}
