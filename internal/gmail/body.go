package gmail

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/jaytaylor/html2text"
	"google.golang.org/api/gmail/v1"
)

// ExtractBody extracts the message body from a Gmail message.
// It prefers text/plain parts, but falls back to converting HTML to text if needed.
// Returns the extracted body text.
func ExtractBody(msg *gmail.Message) (string, error) {
	if msg == nil || msg.Payload == nil {
		return "", fmt.Errorf("message or payload is nil")
	}

	// Try to find text/plain part first
	plainText := findPartByMimeType(msg.Payload, "text/plain")
	if plainText != "" {
		return plainText, nil
	}

	// Fall back to HTML conversion
	htmlText := findPartByMimeType(msg.Payload, "text/html")
	if htmlText != "" {
		text, err := html2text.FromString(htmlText, html2text.Options{
			PrettyTables: false,
			OmitLinks:    false,
		})
		if err != nil {
			return "", fmt.Errorf("failed to convert HTML to text: %w", err)
		}
		return text, nil
	}

	// If no text/plain or text/html found, return the snippet
	return msg.Snippet, nil
}

// findPartByMimeType recursively searches for a MIME part with the given type.
// Returns the decoded body of the first matching part, or empty string if not found.
func findPartByMimeType(part *gmail.MessagePart, mimeType string) string {
	if part == nil {
		return ""
	}

	// Check if this part matches the MIME type
	if part.MimeType == mimeType {
		if part.Body != nil && part.Body.Data != "" {
			decoded, err := decodeBase64URLString(part.Body.Data)
			if err == nil {
				return decoded
			}
		}
	}

	// Recursively search in parts
	for _, subPart := range part.Parts {
		if result := findPartByMimeType(subPart, mimeType); result != "" {
			return result
		}
	}

	return ""
}

// decodeBase64URLString decodes a Gmail base64url-encoded string.
func decodeBase64URLString(s string) (string, error) {
	// Gmail uses base64url encoding (RFC 4648)
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		// Try standard base64 as fallback
		data, err = base64.StdEncoding.DecodeString(s)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64: %w", err)
		}
	}
	return string(data), nil
}

// ExtractBodyWithOptions provides more control over body extraction.
type BodyExtractionOptions struct {
	PreferPlainText bool // If true, prefer text/plain over HTML
	ConvertHTML     bool // If true, convert HTML to text when no plain text is available
	MaxLength       int  // Maximum length of body to return (0 = unlimited)
}

// ExtractBodyWithOptions extracts the message body with custom options.
func ExtractBodyWithOptions(msg *gmail.Message, opts BodyExtractionOptions) (string, error) {
	if msg == nil || msg.Payload == nil {
		return "", fmt.Errorf("message or payload is nil")
	}

	var body string

	if opts.PreferPlainText {
		// Try text/plain first
		body = findPartByMimeType(msg.Payload, "text/plain")
		if body == "" && opts.ConvertHTML {
			// Fall back to HTML conversion
			htmlText := findPartByMimeType(msg.Payload, "text/html")
			if htmlText != "" {
				text, err := html2text.FromString(htmlText, html2text.Options{
					PrettyTables: false,
					OmitLinks:    false,
				})
				if err != nil {
					return "", fmt.Errorf("failed to convert HTML to text: %w", err)
				}
				body = text
			}
		}
	} else {
		// Try HTML first
		htmlText := findPartByMimeType(msg.Payload, "text/html")
		if htmlText != "" && opts.ConvertHTML {
			text, err := html2text.FromString(htmlText, html2text.Options{
				PrettyTables: false,
				OmitLinks:    false,
			})
			if err != nil {
				return "", fmt.Errorf("failed to convert HTML to text: %w", err)
			}
			body = text
		} else if htmlText != "" {
			body = htmlText
		} else {
			// Fall back to text/plain
			body = findPartByMimeType(msg.Payload, "text/plain")
		}
	}

	// If still no body, use snippet
	if body == "" {
		body = msg.Snippet
	}

	// Apply max length if specified
	if opts.MaxLength > 0 && len(body) > opts.MaxLength {
		body = body[:opts.MaxLength]
		// Try to end at a word boundary
		if idx := strings.LastIndex(body, " "); idx > opts.MaxLength-100 {
			body = body[:idx]
		}
		body += "..."
	}

	return body, nil
}

// StripQuotedReply removes quoted reply text from an email body.
// It looks for common reply markers and removes everything after them.
var quotedReplyPatterns = []*regexp.Regexp{
	// Gmail/Outlook style: "On [date], [person] wrote:"
	regexp.MustCompile(`(?m)^On .+wrote:$`),
	// Common reply marker: "-----Original Message-----"
	regexp.MustCompile(`(?m)^-+\s*Original Message\s*-+$`),
	// Common reply marker: "> " at start of line
	regexp.MustCompile(`(?m)^>\s`),
	// Reply line with date
	regexp.MustCompile(`(?m)^.+\d{4}.+wrote:$`),
	// Gmail mobile: "On [date] <email> wrote:"
	regexp.MustCompile(`(?m)^On .+<.+@.+>.+wrote:$`),
	// "From:" style reply header
	regexp.MustCompile(`(?m)^From:\s*.+$`),
	// Horizontal line separators (often used before quoted content)
	regexp.MustCompile(`(?m)^_{3,}$`),
	regexp.MustCompile(`(?m)^-{3,}$`),
}

// StripQuotedReply removes quoted reply content from the email body.
func StripQuotedReply(body string) string {
	// Try each pattern and find the earliest match
	earliestIndex := len(body)
	
	for _, pattern := range quotedReplyPatterns {
		if loc := pattern.FindStringIndex(body); loc != nil && loc[0] < earliestIndex {
			earliestIndex = loc[0]
		}
	}
	
	// If we found a quoted section, trim everything after it
	if earliestIndex < len(body) {
		body = body[:earliestIndex]
	}
	
	// Trim trailing whitespace
	return strings.TrimSpace(body)
}

// TruncateBody truncates the body to the specified maximum length.
// Tries to end at a word boundary for cleaner truncation.
func TruncateBody(body string, maxLength int) string {
	if maxLength <= 0 || len(body) <= maxLength {
		return body
	}
	
	truncated := body[:maxLength]
	
	// Try to end at a word boundary
	if idx := strings.LastIndex(truncated, " "); idx > maxLength-100 && idx > 0 {
		truncated = truncated[:idx]
	}
	
	return strings.TrimSpace(truncated) + "..."
}

// CleanBody performs common cleaning operations on email body text:
// - Strips quoted replies
// - Truncates to max length
// - Normalizes whitespace
func CleanBody(body string, maxLength int) string {
	// Strip quoted replies first
	body = StripQuotedReply(body)
	
	// Normalize whitespace (collapse multiple spaces/newlines)
	body = normalizeWhitespace(body)
	
	// Truncate if needed
	if maxLength > 0 {
		body = TruncateBody(body, maxLength)
	}
	
	return strings.TrimSpace(body)
}

// normalizeWhitespace collapses multiple consecutive whitespace characters.
func normalizeWhitespace(s string) string {
	// Replace multiple spaces with single space
	spacePattern := regexp.MustCompile(`[ \t]+`)
	s = spacePattern.ReplaceAllString(s, " ")
	
	// Replace more than 2 consecutive newlines with 2 newlines
	newlinePattern := regexp.MustCompile(`\n{3,}`)
	s = newlinePattern.ReplaceAllString(s, "\n\n")
	
	return s
}
