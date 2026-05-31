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
	RawMessage  *gmail.Message // raw API message (metadata only — no body; see GetMessage)
}

// GetMessage fetches a message's metadata by ID using format=metadata.
// It returns the message's label IDs and a small allow-list of headers
// (From/To/Subject/Date) but deliberately does NOT fetch the message body —
// mailtagger never reads or transmits private message content.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	var gmailMsg *gmail.Message

	// format=metadata plus a header allow-list ensures the Gmail API never
	// returns the message body to this process.
	err := c.rateLimiter.DoWithOp(ctx, "messages.get", func() error {
		var apiErr error
		gmailMsg, apiErr = c.service.Users.Messages.Get("me", messageID).
			Context(ctx).
			Format("metadata").
			MetadataHeaders("From", "To", "Subject", "Date").
			Do()
		return apiErr
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get message %s: %w", messageID, err)
	}

	return parseMessage(gmailMsg), nil
}

// GetMessageWithBody fetches a message by ID using format=full, including the
// message body. This is used only when the operator opts in via the
// include_body config option; by default GetMessage (metadata only) is used so
// the body is never retrieved.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) GetMessageWithBody(ctx context.Context, messageID string) (*Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	var gmailMsg *gmail.Message

	err := c.rateLimiter.DoWithOp(ctx, "messages.get", func() error {
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

	return parseMessage(gmailMsg), nil
}

// parseMessage converts a raw Gmail API message into our Message struct,
// extracting the common headers we care about.
func parseMessage(gmailMsg *gmail.Message) *Message {
	msg := &Message{
		ID:           gmailMsg.Id,
		ThreadID:     gmailMsg.ThreadId,
		LabelIDs:     gmailMsg.LabelIds,
		Snippet:      gmailMsg.Snippet,
		HistoryID:    fmt.Sprintf("%d", gmailMsg.HistoryId),
		InternalDate: gmailMsg.InternalDate,
		RawMessage:   gmailMsg,
	}

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

	return msg
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

// ListRecentMessageIDs returns up to maxResults of the most recent message IDs
// in the mailbox (newest first). It is used for on-demand classification of
// existing messages (admin backfill), not for the normal history-sync path.
// Uses rate limiting and automatic retry on 429/5xx errors.
func (c *Client) ListRecentMessageIDs(ctx context.Context, maxResults int) ([]string, error) {
	if maxResults <= 0 {
		return nil, nil
	}
	// Gmail caps a single page at 500 results.
	if maxResults > 500 {
		maxResults = 500
	}

	var resp *gmail.ListMessagesResponse
	err := c.rateLimiter.DoWithOp(ctx, "messages.list", func() error {
		var apiErr error
		resp, apiErr = c.service.Users.Messages.List("me").
			Context(ctx).
			MaxResults(int64(maxResults)).
			Do()
		return apiErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	ids := make([]string, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		if m.Id != "" {
			ids = append(ids, m.Id)
		}
	}
	return ids, nil
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
	err := c.rateLimiter.DoWithOp(ctx, "messages.modify", func() error {
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
	err := c.rateLimiter.DoWithOp(ctx, "messages.modify", func() error {
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
	err := c.rateLimiter.DoWithOp(ctx, "messages.modify", func() error {
		_, apiErr := c.service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do()
		return apiErr
	})
	if err != nil {
		return fmt.Errorf("failed to modify labels for message %s: %w", messageID, err)
	}
	return nil
}
