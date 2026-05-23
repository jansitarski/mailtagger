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
func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error) {
	if messageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}

	// Fetch the message with full format to get all headers and body parts
	gmailMsg, err := c.service.Users.Messages.Get("me", messageID).
		Context(ctx).
		Format("full").
		Do()
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
// Returns a map of message ID to Message, and any errors encountered.
func (c *Client) GetMessages(ctx context.Context, messageIDs []string) (map[string]*Message, error) {
	messages := make(map[string]*Message)
	
	for _, msgID := range messageIDs {
		msg, err := c.GetMessage(ctx, msgID)
		if err != nil {
			// Log error but continue with other messages
			return nil, fmt.Errorf("failed to fetch message %s: %w", msgID, err)
		}
		messages[msgID] = msg
	}

	return messages, nil
}
