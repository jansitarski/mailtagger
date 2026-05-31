// Package admin provides the admin web UI and REST API handlers.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jansitarski/mailtagger/internal/store"
)

// Backfiller classifies pre-existing messages for an account on demand.
// It is implemented by the pipeline; admin keeps it as an interface to avoid a
// dependency on the pipeline package.
type Backfiller interface {
	BackfillAccount(ctx context.Context, account *store.Account, maxMessages int, onProgress func(handled, total int)) (int, error)
}

// backfillStatus is the JSON-serializable state of the (single) backfill job.
type backfillStatus struct {
	Running    bool       `json:"running"`
	Account    string     `json:"account,omitempty"`
	Requested  int        `json:"requested,omitempty"`
	Handled    int        `json:"handled"`
	Total      int        `json:"total"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// Handler serves the admin API endpoints.
type Handler struct {
	store      *store.Store
	logger     *slog.Logger
	startTime  time.Time
	dryRun     bool
	backfiller Backfiller

	backfillMu sync.Mutex
	backfill   backfillStatus
}

// NewHandler creates a new admin API handler. backfiller may be nil (the
// "classify previous emails" action is then unavailable).
func NewHandler(st *store.Store, logger *slog.Logger, dryRun bool, backfiller Backfiller) *Handler {
	return &Handler{
		store:      st,
		logger:     logger,
		startTime:  time.Now(),
		dryRun:     dryRun,
		backfiller: backfiller,
	}
}

// Routes registers all admin API routes on the given router.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/status", h.handleStatus)
	r.Get("/accounts", h.handleAccounts)
	r.Get("/history", h.handleHistory)
	r.Post("/reset-cursor", h.handleResetCursor)
	r.Post("/classify-previous", h.handleClassifyPrevious)
	r.Get("/classify-previous/status", h.handleClassifyPreviousStatus)
}

// resolveAccount looks up an account by email address or numeric ID.
func (h *Handler) resolveAccount(identifier string) (*store.Account, error) {
	account, err := h.store.GetAccountByEmail(identifier)
	if err == nil {
		return account, nil
	}
	if err == store.ErrAccountNotFound {
		var id int64
		if _, perr := fmt.Sscanf(identifier, "%d", &id); perr == nil {
			return h.store.GetAccount(id)
		}
	}
	return nil, err
}

// statusResponse is the JSON response for GET /admin/api/status.
type statusResponse struct {
	Status         string `json:"status"`
	Uptime         string `json:"uptime"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	TotalAccounts  int    `json:"total_accounts"`
	TotalProcessed int64  `json:"total_processed"`
	DryRun         bool   `json:"dry_run"`
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	pipelineStatus, err := h.store.GetPipelineStatus()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to get pipeline status: "+err.Error())
		return
	}

	uptime := time.Since(h.startTime)
	resp := statusResponse{
		Status:         "running",
		Uptime:         formatDuration(uptime),
		UptimeSeconds:  int64(uptime.Seconds()),
		TotalAccounts:  pipelineStatus.TotalAccounts,
		TotalProcessed: pipelineStatus.TotalProcessed,
		DryRun:         h.dryRun,
	}

	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.ListAccountStats()
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to list accounts: "+err.Error())
		return
	}

	if stats == nil {
		stats = []store.AccountStats{}
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"accounts": stats,
	})
}

// historyResponse is the JSON response for GET /admin/api/history.
type historyResponse struct {
	Messages []store.ProcessedMessageRecord `json:"messages"`
	Total    int64                          `json:"total"`
	Limit    int                            `json:"limit"`
	Offset   int                            `json:"offset"`
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	// Parse query params
	limit := 50
	offset := 0
	var accountID int64

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	if a := r.URL.Query().Get("account_id"); a != "" {
		if parsed, err := strconv.ParseInt(a, 10, 64); err == nil && parsed > 0 {
			accountID = parsed
		}
	}

	messages, total, err := h.store.GetRecentProcessedMessages(accountID, limit, offset)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to get history: "+err.Error())
		return
	}

	if messages == nil {
		messages = []store.ProcessedMessageRecord{}
	}

	h.respondJSON(w, http.StatusOK, historyResponse{
		Messages: messages,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// resetCursorRequest is the JSON body for POST /admin/api/reset-cursor.
type resetCursorRequest struct {
	Account        string `json:"account"`         // email, numeric ID, or "all"
	ClearProcessed bool   `json:"clear_processed"` // also clear processed_messages
}

func (h *Handler) handleResetCursor(w http.ResponseWriter, r *http.Request) {
	var req resetCursorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Account == "" {
		h.respondError(w, http.StatusBadRequest, "account field is required")
		return
	}

	if req.Account == "all" {
		resetCount, err := h.store.ResetAllHistoryIDs()
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "failed to reset cursors: "+err.Error())
			return
		}

		var clearedCount int64
		if req.ClearProcessed {
			clearedCount, err = h.store.DeleteAllProcessedMessages()
			if err != nil {
				h.respondError(w, http.StatusInternalServerError, "failed to clear processed messages: "+err.Error())
				return
			}
		}

		h.logger.Info("admin: reset all cursors", "reset_count", resetCount, "cleared_processed", clearedCount)
		h.respondJSON(w, http.StatusOK, map[string]interface{}{
			"status":            "ok",
			"accounts_reset":    resetCount,
			"messages_cleared":  clearedCount,
		})
		return
	}

	// Find account by email or numeric ID
	var account *store.Account
	var err error
	account, err = h.store.GetAccountByEmail(req.Account)
	if err == store.ErrAccountNotFound {
		var id int64
		if _, parseErr := fmt.Sscanf(req.Account, "%d", &id); parseErr == nil {
			account, err = h.store.GetAccount(id)
		}
	}
	if err != nil {
		if err == store.ErrAccountNotFound {
			h.respondError(w, http.StatusNotFound, "account not found: "+req.Account)
			return
		}
		h.respondError(w, http.StatusInternalServerError, "failed to find account: "+err.Error())
		return
	}

	if err := h.store.ResetHistoryID(account.ID); err != nil {
		h.respondError(w, http.StatusInternalServerError, "failed to reset cursor: "+err.Error())
		return
	}

	var clearedCount int64
	if req.ClearProcessed {
		clearedCount, err = h.store.DeleteProcessedMessages(account.ID)
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "failed to clear processed messages: "+err.Error())
			return
		}
	}

	h.logger.Info("admin: reset cursor", "account", account.Email, "cleared_processed", clearedCount)
	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "ok",
		"account":          account.Email,
		"messages_cleared": clearedCount,
	})
}

// classifyPreviousRequest is the JSON body for POST /admin/api/classify-previous.
type classifyPreviousRequest struct {
	Account     string `json:"account"`      // email address or numeric ID
	MaxMessages int    `json:"max_messages"` // most-recent messages to scan (default 50, max 500)
}

// handleClassifyPrevious starts a background job that classifies the account's
// most recent existing emails. Only one job runs at a time.
func (h *Handler) handleClassifyPrevious(w http.ResponseWriter, r *http.Request) {
	if h.backfiller == nil {
		h.respondError(w, http.StatusServiceUnavailable, "classification is not available")
		return
	}

	var req classifyPreviousRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Account == "" {
		h.respondError(w, http.StatusBadRequest, "account field is required")
		return
	}
	if req.MaxMessages <= 0 {
		req.MaxMessages = 50
	}
	if req.MaxMessages > 500 {
		req.MaxMessages = 500
	}

	account, err := h.resolveAccount(req.Account)
	if err != nil {
		if err == store.ErrAccountNotFound {
			h.respondError(w, http.StatusNotFound, "account not found: "+req.Account)
			return
		}
		h.respondError(w, http.StatusInternalServerError, "failed to find account: "+err.Error())
		return
	}

	h.backfillMu.Lock()
	if h.backfill.Running {
		h.backfillMu.Unlock()
		h.respondError(w, http.StatusConflict, "a classification run is already in progress")
		return
	}
	started := time.Now()
	h.backfill = backfillStatus{
		Running:   true,
		Account:   account.Email,
		Requested: req.MaxMessages,
		StartedAt: &started,
	}
	h.backfillMu.Unlock()

	go h.runBackfill(account, req.MaxMessages)

	h.logger.Info("admin: classify previous emails started", "account", account.Email, "max_messages", req.MaxMessages)
	h.respondJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":       "started",
		"account":      account.Email,
		"max_messages": req.MaxMessages,
	})
}

// runBackfill executes the backfill in the background and records its status.
func (h *Handler) runBackfill(account *store.Account, maxMessages int) {
	// Use a detached context so the job outlives the triggering HTTP request,
	// with a generous cap so it can never run indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	handled, err := h.backfiller.BackfillAccount(ctx, account, maxMessages, func(handled, total int) {
		h.backfillMu.Lock()
		h.backfill.Handled = handled
		h.backfill.Total = total
		h.backfillMu.Unlock()
	})

	finished := time.Now()
	h.backfillMu.Lock()
	h.backfill.Running = false
	h.backfill.Handled = handled
	h.backfill.FinishedAt = &finished
	if err != nil && err != context.Canceled {
		h.backfill.Error = err.Error()
	}
	h.backfillMu.Unlock()

	h.logger.Info("admin: classify previous emails finished", "account", account.Email, "handled", handled, "error", err)
}

// handleClassifyPreviousStatus returns the current/last backfill job status.
func (h *Handler) handleClassifyPreviousStatus(w http.ResponseWriter, r *http.Request) {
	h.backfillMu.Lock()
	status := h.backfill
	h.backfillMu.Unlock()
	h.respondJSON(w, http.StatusOK, status)
}

func (h *Handler) respondJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
