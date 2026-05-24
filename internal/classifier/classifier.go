// Package classifier provides LLM-based email classification.
package classifier

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tmc/langchaingo/llms"
)

// Email represents an email message to be classified.
type Email struct {
	ID      string // unique message identifier
	From    string // sender address
	Subject string // email subject
	Body    string // email body (plain text)
}

// Decision represents the classification result for an email.
type Decision struct {
	Category   string  `json:"category"`   // assigned category name
	Confidence float64 `json:"confidence"` // confidence score 0.0-1.0
	Reasoning  string  `json:"reasoning"`  // brief explanation from LLM
}

// Category defines a classification category for the LLM.
type Category struct {
	Name        string // unique category name
	Description string // description for the LLM classifier
}

// Classifier provides LLM-based email classification.
type Classifier struct {
	model      llms.Model // LLM model for classification
	categories []Category // available classification categories
}

// New creates a new Classifier with the given model and categories.
func New(model llms.Model, categories []Category) *Classifier {
	return &Classifier{
		model:      model,
		categories: categories,
	}
}

// Classify classifies the given email and returns a Decision.
func (c *Classifier) Classify(ctx context.Context, email Email) (*Decision, error) {
	// Render the system prompt with categories
	systemPrompt, err := RenderSystemPrompt(DefaultSystemPrompt, c.categories)
	if err != nil {
		return nil, fmt.Errorf("render system prompt: %w", err)
	}

	// Build the user message with email content
	userPrompt := fmt.Sprintf("From: %s\nSubject: %s\n\n%s", email.From, email.Subject, email.Body)

	// Call the LLM with JSON mode
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
		llms.TextParts(llms.ChatMessageTypeHuman, userPrompt),
	}

	response, err := c.model.GenerateContent(ctx, messages, llms.WithJSONMode())
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response from LLM")
	}

	// Parse the JSON response
	var decision Decision
	if err := json.Unmarshal([]byte(response.Choices[0].Content), &decision); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	return &decision, nil
}
