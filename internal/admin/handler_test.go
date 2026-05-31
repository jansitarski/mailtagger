package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jansitarski/mailtagger/internal/store"
)

// fakeBackfiller is a controllable Backfiller for tests. It blocks in
// BackfillAccount until proceed is closed, so tests can observe the "running"
// state deterministically.
type fakeBackfiller struct {
	proceed chan struct{}
}

func (f *fakeBackfiller) BackfillAccount(ctx context.Context, account *store.Account, maxMessages int, onProgress func(handled, total int)) (int, error) {
	if onProgress != nil {
		onProgress(0, maxMessages)
	}
	select {
	case <-f.proceed:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	if onProgress != nil {
		onProgress(maxMessages, maxMessages)
	}
	return maxMessages, nil
}

func newTestStoreWithAccount(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:", 30)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := st.InsertAccount("user@example.com", []byte("tok")); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return st
}

func postClassify(h *Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/classify-previous", bytes.NewBufferString(body))
	h.handleClassifyPrevious(rec, req)
	return rec
}

func getStatus(h *Handler) backfillStatus {
	rec := httptest.NewRecorder()
	h.handleClassifyPreviousStatus(rec, httptest.NewRequest(http.MethodGet, "/admin/api/classify-previous/status", nil))
	var st backfillStatus
	json.NewDecoder(rec.Body).Decode(&st)
	return st
}

func TestClassifyPrevious_NoBackfiller(t *testing.T) {
	st := newTestStoreWithAccount(t)
	defer st.Close()
	h := NewHandler(st, slog.Default(), false, nil)

	rec := postClassify(h, `{"account":"user@example.com"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestClassifyPrevious_Validation(t *testing.T) {
	st := newTestStoreWithAccount(t)
	defer st.Close()
	h := NewHandler(st, slog.Default(), false, &fakeBackfiller{proceed: make(chan struct{})})

	if rec := postClassify(h, `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty account: expected 400, got %d", rec.Code)
	}
	if rec := postClassify(h, `{"account":"missing@example.com"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown account: expected 404, got %d", rec.Code)
	}
}

func TestClassifyPrevious_RunFlow(t *testing.T) {
	st := newTestStoreWithAccount(t)
	defer st.Close()
	fb := &fakeBackfiller{proceed: make(chan struct{})}
	h := NewHandler(st, slog.Default(), false, fb)

	// Start a run.
	rec := postClassify(h, `{"account":"user@example.com","max_messages":5}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Running state is set synchronously before the handler returns.
	if st := getStatus(h); !st.Running || st.Account != "user@example.com" {
		t.Fatalf("expected running status for account, got %+v", st)
	}

	// A second run while one is in progress is rejected.
	if rec := postClassify(h, `{"account":"user@example.com"}`); rec.Code != http.StatusConflict {
		t.Errorf("concurrent run: expected 409, got %d", rec.Code)
	}

	// Let the job finish and wait for completion.
	close(fb.proceed)
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := getStatus(h)
		if !status.Running {
			if status.Handled != 5 {
				t.Errorf("expected handled=5, got %d", status.Handled)
			}
			if status.Error != "" {
				t.Errorf("unexpected error: %s", status.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for backfill to finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClassifyPrevious_MaxMessagesClamped(t *testing.T) {
	st := newTestStoreWithAccount(t)
	defer st.Close()
	fb := &fakeBackfiller{proceed: make(chan struct{})}
	close(fb.proceed) // finish immediately
	h := NewHandler(st, slog.Default(), false, fb)

	rec := postClassify(h, `{"account":"user@example.com","max_messages":99999}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["max_messages"].(float64) != 500 {
		t.Errorf("expected max_messages clamped to 500, got %v", resp["max_messages"])
	}
}
