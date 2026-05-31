package gmail

import (
	"context"
	"errors"
	"testing"
)

// memLabelCache is an in-memory LabelCache for tests.
type memLabelCache struct {
	byName map[string]string
}

func (m *memLabelCache) GetLabel(accountID int64, labelName string) (string, error) {
	if id, ok := m.byName[labelName]; ok {
		return id, nil
	}
	return "", errors.New("label not found")
}

func (m *memLabelCache) UpsertLabel(accountID int64, labelName, labelID string) error {
	m.byName[labelName] = labelID
	return nil
}

func (m *memLabelCache) ListLabels(accountID int64) ([]CachedLabel, error) {
	out := make([]CachedLabel, 0, len(m.byName))
	for name, id := range m.byName {
		out = append(out, CachedLabel{Name: name, ID: id})
	}
	return out, nil
}

// TestGetOrCreateLabel_CacheHit verifies the fast path returns the cached ID
// without making any Gmail API call (the client has no service/rate limiter).
func TestGetOrCreateLabel_CacheHit(t *testing.T) {
	cache := &memLabelCache{byName: map[string]string{"automated/notification": "Label_42"}}
	lm := NewLabelManager(&Client{}, cache, 1)

	id, err := lm.GetOrCreateLabel(context.Background(), "automated/notification")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "Label_42" {
		t.Errorf("expected cached label ID Label_42, got %q", id)
	}
}
