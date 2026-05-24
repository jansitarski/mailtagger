// Package classifier provides LLM-based email classification.
package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tmc/langchaingo/llms"
)

// FallbackCategory is the default category used when LLM returns an unknown category.
const FallbackCategory = "Others"

// ErrNilModel is returned when attempting to classify with a nil model.
var ErrNilModel = errors.New("classifier model is nil")

// ErrMissingFallbackCategory is returned when the fallback category is not in the category list.
var ErrMissingFallbackCategory = errors.New("fallback category 'Others' must be included in categories")

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
	model            llms.Model // LLM model for classification
	categories       []Category // available classification categories
	systemPromptTmpl string     // custom system prompt template (optional)
}

// New creates a new Classifier with the given model and categories.
// Returns an error if the fallback category "Others" is not in the category list.
func New(model llms.Model, categories []Category) (*Classifier, error) {
	// Validate that the fallback category exists
	hasFallback := false
	for _, cat := range categories {
		if cat.Name == FallbackCategory {
			hasFallback = true
			break
		}
	}
	if !hasFallback {
		return nil, ErrMissingFallbackCategory
	}

	return &Classifier{
		model:      model,
		categories: categories,
	}, nil
}

// WithSystemPrompt sets a custom system prompt template for the Classifier.
func (c *Classifier) WithSystemPrompt(tmpl string) *Classifier {
	c.systemPromptTmpl = tmpl
	return c
}

// Classify classifies the given email and returns a Decision.
func (c *Classifier) Classify(ctx context.Context, email Email) (*Decision, error) {
	if c.model == nil {
		return nil, ErrNilModel
	}

	// Use custom or default system prompt template
	tmpl := c.systemPromptTmpl
	if tmpl == "" {
		tmpl = DefaultSystemPrompt
	}

	// Render the system prompt with categories
	systemPrompt, err := RenderSystemPrompt(tmpl, c.categories)
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

	// Validate the category
	decision.Category = c.validateCategory(decision.Category)

	return &decision, nil
}

// validateCategory checks if the category is valid and returns the fallback if not.
func (c *Classifier) validateCategory(category string) string {
	for _, cat := range c.categories {
		if cat.Name == category {
			return category
		}
	}
	// Hallucinated category, use fallback
	return FallbackCategory
}
