package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/jansitarski/mailtagger/internal/classifier"
	"github.com/jansitarski/mailtagger/internal/config"
	"github.com/jansitarski/mailtagger/internal/gmail"
	"github.com/jansitarski/mailtagger/internal/store"
	"github.com/tmc/langchaingo/llms"
)

// mockGmailClient implements a mock Gmail client for testing.
type mockGmailClient struct {
	messages       map[string]*gmail.Message
	historyID      string
	newMessageIDs  []string
	appliedLabels  map[string][]string
	createdLabels  map[string]string
}

func newMockGmailClient() *mockGmailClient {
	return &mockGmailClient{
		messages:      make(map[string]*gmail.Message),
		appliedLabels: make(map[string][]string),
		createdLabels: make(map[string]string),
		historyID:     "12345",
	}
}

func (m *mockGmailClient) GetCurrentHistoryID(ctx context.Context) (string, error) {
	return m.historyID, nil
}

func (m *mockGmailClient) SyncHistory(ctx context.Context, startHistoryID string) (*gmail.HistoryResult, error) {
	return &gmail.HistoryResult{
		MessageIDs:    m.newMessageIDs,
		NextHistoryID: m.historyID,
	}, nil
}

func (m *mockGmailClient) GetMessage(ctx context.Context, messageID string) (*gmail.Message, error) {
	if msg, ok := m.messages[messageID]; ok {
		return msg, nil
	}
	return &gmail.Message{
		ID:       messageID,
		From:     "test@example.com",
		Subject:  "Test Subject",
		Snippet:  "Test message body",
		LabelIDs: []string{},
	}, nil
}

func (m *mockGmailClient) AddLabels(ctx context.Context, messageID string, labelIDs []string) error {
	m.appliedLabels[messageID] = append(m.appliedLabels[messageID], labelIDs...)
	return nil
}

// mockGmailClientFactory creates mock Gmail clients.
type mockGmailClientFactory struct {
	client *mockGmailClient
}

func (f *mockGmailClientFactory) NewClient(ctx context.Context, account *store.Account) (*gmail.Client, error) {
	// Return nil - we'll use the mock directly in tests
	return nil, nil
}

// mockLLM implements a mock LLM for testing.
type mockLLM struct {
	category   string
	confidence float64
}

func (m *mockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: `{"category": "` + m.category + `", "confidence": 0.9, "reasoning": "test"}`,
			},
		},
	}, nil
}

func (m *mockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return `{"category": "` + m.category + `", "confidence": 0.9, "reasoning": "test"}`, nil
}

func TestPipelineNew(t *testing.T) {
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}

	cfg := &config.Config{
		Categories: []config.Category{
			{Name: "Work", Label: "AI/Work", Description: "Work emails"},
			{Name: "Personal", Label: "AI/Personal", Description: "Personal emails"},
			{Name: "Others", Label: "AI/Others", Description: "Other emails"},
		},
		MaxMessagesPerTick: 10,
	}

	mockLLM := &mockLLM{category: "Work", confidence: 0.9}
	cls, err := classifier.New(mockLLM, []classifier.Category{
		{Name: "Work", Description: "Work emails"},
		{Name: "Personal", Description: "Personal emails"},
		{Name: "Others", Description: "Other emails"},
	})
	if err != nil {
		t.Fatalf("failed to create classifier: %v", err)
	}

	factory := &mockGmailClientFactory{client: newMockGmailClient()}

	p := New(st, cls, factory, cfg)

	if p == nil {
		t.Fatal("expected pipeline to be created")
	}

	if p.Store() != st {
		t.Error("store mismatch")
	}

	if p.Classifier() != cls {
		t.Error("classifier mismatch")
	}

	if p.Config() != cfg {
		t.Error("config mismatch")
	}
}

func TestPipelineGetLabelForCategory(t *testing.T) {
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	cfg := &config.Config{
		Categories: []config.Category{
			{Name: "Work", Label: "AI/Work", Description: "Work emails"},
			{Name: "Personal", Label: "AI/Personal", Description: "Personal emails"},
			{Name: "Others", Label: "AI/Others", Description: "Other emails"},
		},
	}

	p := New(st, nil, nil, cfg)

	tests := []struct {
		category string
		want     string
	}{
		{"Work", "AI/Work"},
		{"Personal", "AI/Personal"},
		{"Others", "AI/Others"},
		{"Unknown", ""},
	}

	for _, tt := range tests {
		got := p.GetLabelForCategory(tt.category)
		if got != tt.want {
			t.Errorf("GetLabelForCategory(%q) = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestPipelineCacheAILabelIDs(t *testing.T) {
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	cfg := &config.Config{
		Categories: []config.Category{
			{Name: "Work", Label: "AI/Work", Description: "Work emails"},
			{Name: "Others", Label: "AI/Others", Description: "Other emails"},
		},
	}

	p := New(st, nil, nil, cfg)

	// Cache AI label IDs
	labelIDs := map[string]string{
		"AI/Work":   "Label_123",
		"AI/Others": "Label_456",
		"INBOX":     "INBOX",
	}
	p.cacheAILabelIDs(1, labelIDs)

	// Test hasAILabel
	msg := &gmail.Message{
		LabelIDs: []string{"Label_123", "INBOX"},
	}

	if !p.hasAILabel(1, msg) {
		t.Error("expected hasAILabel to return true for message with AI label")
	}

	msg2 := &gmail.Message{
		LabelIDs: []string{"INBOX", "SENT"},
	}

	if p.hasAILabel(1, msg2) {
		t.Error("expected hasAILabel to return false for message without AI label")
	}

	// Test with unknown account
	if p.hasAILabel(999, msg) {
		t.Error("expected hasAILabel to return false for unknown account")
	}
}

func TestPipelineRunContextCancellation(t *testing.T) {
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}

	cfg := &config.Config{
		Categories: []config.Category{
			{Name: "Others", Label: "AI/Others", Description: "Other emails"},
		},
		Accounts: []config.AccountConfig{
			{ID: "test", Email: "test@example.com", PollInterval: "100ms"},
		},
	}

	mockLLM := &mockLLM{category: "Others", confidence: 0.9}
	cls, err := classifier.New(mockLLM, []classifier.Category{
		{Name: "Others", Description: "Other emails"},
	})
	if err != nil {
		t.Fatalf("failed to create classifier: %v", err)
	}

	factory := &mockGmailClientFactory{client: newMockGmailClient()}

	p := New(st, cls, factory, cfg)

	// Create a context that will be cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Run should return after context is cancelled
	done := make(chan error)
	go func() {
		done <- p.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on context cancellation, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("Run did not return after context cancellation")
	}
}

func TestPipelineTickContextCancellation(t *testing.T) {
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}

	cfg := &config.Config{
		Categories: []config.Category{
			{Name: "Others", Label: "AI/Others", Description: "Other emails"},
		},
	}

	p := New(st, nil, nil, cfg)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = p.tick(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestDefaultMaxMessagesPerTick(t *testing.T) {
	if DefaultMaxMessagesPerTick != 50 {
		t.Errorf("expected DefaultMaxMessagesPerTick to be 50, got %d", DefaultMaxMessagesPerTick)
	}
}

func TestDefaultPollInterval(t *testing.T) {
	if DefaultPollInterval != 5*time.Minute {
		t.Errorf("expected DefaultPollInterval to be 5m, got %v", DefaultPollInterval)
	}
}

func TestAILabelPrefix(t *testing.T) {
	if AILabelPrefix != "AI/" {
		t.Errorf("expected AILabelPrefix to be 'AI/', got %q", AILabelPrefix)
	}
}

// Test that store adapter implements gmail.LabelCache interface
func TestStoreLabelCacheInterface(t *testing.T) {
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}

	// Create an account for testing
	_, err = st.InsertAccount("test@example.com", []byte("token"))
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	cache := &storeLabelCache{store: st}

	// Test UpsertLabel
	err = cache.UpsertLabel(1, "AI/Work", "Label_123")
	if err != nil {
		t.Errorf("UpsertLabel failed: %v", err)
	}

	// Test GetLabel
	labelID, err := cache.GetLabel(1, "AI/Work")
	if err != nil {
		t.Errorf("GetLabel failed: %v", err)
	}
	if labelID != "Label_123" {
		t.Errorf("expected label ID 'Label_123', got %q", labelID)
	}

	// Test ListLabels
	labels, err := cache.ListLabels(1)
	if err != nil {
		t.Errorf("ListLabels failed: %v", err)
	}
	if len(labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(labels))
	}
	if labels[0].Name != "AI/Work" {
		t.Errorf("expected label name 'AI/Work', got %q", labels[0].Name)
	}
}
