package gmail

import (
	"encoding/base64"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestExtractBody_PlainText(t *testing.T) {
	msg := &gmail.Message{
		Payload: &gmail.MessagePart{
			MimeType: "multipart/alternative",
			Parts: []*gmail.MessagePart{
				{
					MimeType: "text/plain",
					Body: &gmail.MessagePartBody{
						Data: encodeBase64URL("This is plain text content"),
					},
				},
				{
					MimeType: "text/html",
					Body: &gmail.MessagePartBody{
						Data: encodeBase64URL("<p>This is HTML content</p>"),
					},
				},
			},
		},
	}

	body, err := ExtractBody(msg)
	if err != nil {
		t.Fatalf("ExtractBody failed: %v", err)
	}

	expected := "This is plain text content"
	if body != expected {
		t.Errorf("Expected %q, got %q", expected, body)
	}
}

func TestExtractBody_HTMLFallback(t *testing.T) {
	msg := &gmail.Message{
		Payload: &gmail.MessagePart{
			MimeType: "text/html",
			Body: &gmail.MessagePartBody{
				Data: encodeBase64URL("<p>This is <strong>HTML</strong> content</p>"),
			},
		},
	}

	body, err := ExtractBody(msg)
	if err != nil {
		t.Fatalf("ExtractBody failed: %v", err)
	}

	// html2text should convert HTML to plain text
	if body == "" {
		t.Error("Expected non-empty body after HTML conversion")
	}
}

func TestExtractBody_Nested(t *testing.T) {
	msg := &gmail.Message{
		Payload: &gmail.MessagePart{
			MimeType: "multipart/mixed",
			Parts: []*gmail.MessagePart{
				{
					MimeType: "multipart/alternative",
					Parts: []*gmail.MessagePart{
						{
							MimeType: "text/plain",
							Body: &gmail.MessagePartBody{
								Data: encodeBase64URL("Nested plain text"),
							},
						},
					},
				},
			},
		},
	}

	body, err := ExtractBody(msg)
	if err != nil {
		t.Fatalf("ExtractBody failed: %v", err)
	}

	expected := "Nested plain text"
	if body != expected {
		t.Errorf("Expected %q, got %q", expected, body)
	}
}

func TestStripQuotedReply(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Gmail style",
			input: `This is my reply.

On Tue, Jan 1, 2024 at 10:00 AM John Doe <john@example.com> wrote:
This is the original message.`,
			expected: "This is my reply.",
		},
		{
			name: "Original Message separator",
			input: `My response here.

-----Original Message-----
From: sender@example.com
Subject: Re: Topic`,
			expected: "My response here.",
		},
		{
			name: "Quote marker",
			input: `New content.

> This is quoted
> text from previous email`,
			expected: "New content.",
		},
		{
			name: "No quoted content",
			input: "Just a regular email with no replies.",
			expected: "Just a regular email with no replies.",
		},
		{
			name: "Horizontal line separator",
			input: `My message.

___________
Previous message content`,
			expected: "My message.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripQuotedReply(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTruncateBody(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
	}{
		{
			name:      "No truncation needed",
			input:     "Short text",
			maxLength: 100,
			expected:  "Short text",
		},
		{
			name:      "Truncate at word boundary",
			input:     "This is a longer text that needs to be truncated",
			maxLength: 20,
			expected:  "This is a longer...",
		},
		{
			name:      "Zero max length",
			input:     "Some text",
			maxLength: 0,
			expected:  "Some text",
		},
		{
			name:      "Exact length",
			input:     "12345",
			maxLength: 5,
			expected:  "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TruncateBody(tt.input, tt.maxLength)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCleanBody(t *testing.T) {
	input := `My new message here.

On Jan 1, 2024, sender wrote:
Original message with lots of quoted content.
More quoted content.`

	result := CleanBody(input, 50)

	// Should strip quoted content and truncate
	if len(result) > 53 { // 50 + "..."
		t.Errorf("Expected body to be truncated to ~50 chars, got %d", len(result))
	}

	// Should not contain quoted content
	if containsSubstring(result, "Original message") {
		t.Error("Expected quoted content to be stripped")
	}
}

// Helper function to encode string to base64url
func encodeBase64URL(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
