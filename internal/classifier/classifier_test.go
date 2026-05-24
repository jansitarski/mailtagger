package classifier

import (
	"context"
	"errors"
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

// validCategories returns a category list that includes the required fallback category.
func validCategories() []Category {
	return []Category{
		{Name: "Newsletter", Description: "Marketing emails"},
		{Name: "Work", Description: "Work-related emails"},
		{Name: FallbackCategory, Description: "Uncategorized emails"},
	}
}

func TestNew_RequiresFallbackCategory(t *testing.T) {
	// Categories without "Others" should fail
	categories := []Category{
		{Name: "Newsletter", Description: "Marketing emails"},
		{Name: "Work", Description: "Work-related emails"},
	}
	_, err := New(nil, categories)
	if !errors.Is(err, ErrMissingFallbackCategory) {
		t.Errorf("expected ErrMissingFallbackCategory, got %v", err)
	}

	// Categories with "Others" should succeed
	categories = append(categories, Category{Name: FallbackCategory, Description: "Other emails"})
	classifier, err := New(nil, categories)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if classifier == nil {
		t.Fatal("expected classifier, got nil")
	}
}

func TestClassify_ValidJSON(t *testing.T) {
	model := &mockModel{
		response: `{"category": "Newsletter", "confidence": 0.95, "reasoning": "This is a newsletter."}`,
	}
	classifier, err := New(model, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

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
	classifier, err := New(model, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

	email := Email{
		ID:      "123",
		From:    "news@example.com",
		Subject: "Weekly Newsletter",
		Body:    "Check out our latest updates!",
	}

	_, err = classifier.Classify(context.Background(), email)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestClassify_UnknownCategory(t *testing.T) {
	model := &mockModel{
		response: `{"category": "Hallucinated", "confidence": 0.8, "reasoning": "Made up category."}`,
	}
	classifier, err := New(model, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

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

func TestClassify_NilModel(t *testing.T) {
	classifier, err := New(nil, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

	email := Email{
		ID:      "123",
		From:    "test@example.com",
		Subject: "Test",
		Body:    "Test body",
	}

	_, err = classifier.Classify(context.Background(), email)
	if !errors.Is(err, ErrNilModel) {
		t.Errorf("expected ErrNilModel, got %v", err)
	}
}

func TestWithSystemPrompt(t *testing.T) {
	model := &mockModel{
		response: `{"category": "Newsletter", "confidence": 0.9, "reasoning": "Custom prompt used."}`,
	}
	classifier, err := New(model, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

	customPrompt := `Custom classifier prompt.
Categories:
{{range .Categories}}- {{.Name}}
{{end}}
Return JSON with category, confidence, reasoning.`

	classifier.WithSystemPrompt(customPrompt)

	email := Email{
		ID:      "123",
		From:    "test@example.com",
		Subject: "Test",
		Body:    "Test body",
	}

	decision, err := classifier.Classify(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Category != "Newsletter" {
		t.Errorf("expected category Newsletter, got %s", decision.Category)
	}
}

func TestWithSystemPrompt_InvalidTemplate(t *testing.T) {
	model := &mockModel{
		response: `{"category": "Newsletter", "confidence": 0.9, "reasoning": "Test."}`,
	}
	classifier, err := New(model, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

	// Invalid template syntax
	classifier.WithSystemPrompt(`{{invalid template`)

	email := Email{
		ID:      "123",
		From:    "test@example.com",
		Subject: "Test",
		Body:    "Test body",
	}

	_, err = classifier.Classify(context.Background(), email)
	if err == nil {
		t.Fatal("expected error for invalid template, got nil")
	}
}

func TestValidateCategory(t *testing.T) {
	classifier, err := New(nil, validCategories())
	if err != nil {
		t.Fatalf("unexpected error creating classifier: %v", err)
	}

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
