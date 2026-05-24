package classifier

import (
	"context"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

// mockModel is a test double for llms.Model.
type mockModel struct {
	response string
	err      error
}

func (m *mockModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: m.response},
		},
	}, nil
}

func (m *mockModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return m.response, m.err
}

func TestClassify_ValidJSON(t *testing.T) {
	model := &mockModel{
		response: `{"category": "Newsletter", "confidence": 0.95, "reasoning": "This is a newsletter."}`,
	}
	categories := []Category{
		{Name: "Newsletter", Description: "Marketing emails"},
		{Name: "Work", Description: "Work-related emails"},
	}
	classifier := New(model, categories)

	email := Email{
		ID:      "123",
		From:    "news@example.com",
		Subject: "Weekly Newsletter",
		Body:    "Check out our latest updates!",
	}

	decision, err := classifier.Classify(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Category != "Newsletter" {
		t.Errorf("expected category Newsletter, got %s", decision.Category)
	}
	if decision.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", decision.Confidence)
	}
	if decision.Reasoning != "This is a newsletter." {
		t.Errorf("expected reasoning 'This is a newsletter.', got %s", decision.Reasoning)
	}
}

func TestClassify_InvalidJSON(t *testing.T) {
	model := &mockModel{
		response: `not valid json`,
	}
	categories := []Category{
		{Name: "Newsletter", Description: "Marketing emails"},
	}
	classifier := New(model, categories)

	email := Email{
		ID:      "123",
		From:    "news@example.com",
		Subject: "Weekly Newsletter",
		Body:    "Check out our latest updates!",
	}

	_, err := classifier.Classify(context.Background(), email)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestClassify_UnknownCategory(t *testing.T) {
	model := &mockModel{
		response: `{"category": "Hallucinated", "confidence": 0.8, "reasoning": "Made up category."}`,
	}
	categories := []Category{
		{Name: "Newsletter", Description: "Marketing emails"},
		{Name: "Work", Description: "Work-related emails"},
	}
	classifier := New(model, categories)

	email := Email{
		ID:      "123",
		From:    "unknown@example.com",
		Subject: "Test",
		Body:    "Test body",
	}

	decision, err := classifier.Classify(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fallback to Others for unknown category
	if decision.Category != FallbackCategory {
		t.Errorf("expected category %s for hallucinated category, got %s", FallbackCategory, decision.Category)
	}
}

func TestValidateCategory(t *testing.T) {
	categories := []Category{
		{Name: "Newsletter", Description: "Marketing emails"},
		{Name: "Work", Description: "Work-related emails"},
	}
	classifier := New(nil, categories)

	tests := []struct {
		input    string
		expected string
	}{
		{"Newsletter", "Newsletter"},
		{"Work", "Work"},
		{"Unknown", FallbackCategory},
		{"", FallbackCategory},
	}

	for _, tc := range tests {
		result := classifier.validateCategory(tc.input)
		if result != tc.expected {
			t.Errorf("validateCategory(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
