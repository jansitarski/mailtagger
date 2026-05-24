// Package classifier provides LLM-based email classification.
package classifier

import "github.com/tmc/langchaingo/llms"

// Email represents an email message to be classified.
type Email struct {
	ID      string // unique message identifier
	From    string // sender address
	Subject string // email subject
	Body    string // email body (plain text)
}

// Decision represents the classification result for an email.
type Decision struct {
	Category   string  // assigned category name
	Confidence float64 // confidence score 0.0-1.0
	Reasoning  string  // brief explanation from LLM
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
