package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestMessagesProcessedTotal(t *testing.T) {
	// Record some metrics
	MessagesProcessedTotal.WithLabelValues("test@example.com", "AI/Newsletters").Inc()
	MessagesProcessedTotal.WithLabelValues("test@example.com", "AI/Newsletters").Inc()
	MessagesProcessedTotal.WithLabelValues("test@example.com", "AI/Personal").Inc()

	// Verify we can gather the metrics
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// Find our metric
	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_messages_processed_total" {
			found = true
			if len(mf.GetMetric()) == 0 {
				t.Error("expected at least one metric series")
			}
			break
		}
	}

	if !found {
		t.Error("mailtagger_messages_processed_total metric not found")
	}
}

func TestLLMRequestDurationSeconds(t *testing.T) {
	// Record a latency observation
	LLMRequestDurationSeconds.WithLabelValues("openai", "gpt-4").Observe(1.5)

	// Verify we can gather the metrics
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_llm_request_duration_seconds" {
			found = true
			if len(mf.GetMetric()) == 0 {
				t.Error("expected at least one metric series")
			}
			// Check that it's a histogram type
			if mf.GetType() != dto.MetricType_HISTOGRAM {
				t.Errorf("expected histogram type, got %v", mf.GetType())
			}
			break
		}
	}

	if !found {
		t.Error("mailtagger_llm_request_duration_seconds metric not found")
	}
}

func TestLLMErrorsTotal(t *testing.T) {
	LLMErrorsTotal.WithLabelValues("anthropic", "rate_limit").Inc()

	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_llm_errors_total" {
			found = true
			break
		}
	}

	if !found {
		t.Error("mailtagger_llm_errors_total metric not found")
	}
}

func TestGmailAPIErrorsTotal(t *testing.T) {
	GmailAPIErrorsTotal.WithLabelValues("GetMessage", "404").Inc()

	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_gmail_api_errors_total" {
			found = true
			break
		}
	}

	if !found {
		t.Error("mailtagger_gmail_api_errors_total metric not found")
	}
}

func TestHistoryCursorAgeSeconds(t *testing.T) {
	HistoryCursorAgeSeconds.WithLabelValues("test@example.com").Set(300)

	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_history_cursor_age_seconds" {
			found = true
			if mf.GetType() != dto.MetricType_GAUGE {
				t.Errorf("expected gauge type, got %v", mf.GetType())
			}
			break
		}
	}

	if !found {
		t.Error("mailtagger_history_cursor_age_seconds metric not found")
	}
}

func TestProcessedMessagesDBRows(t *testing.T) {
	ProcessedMessagesDBRows.WithLabelValues("test@example.com").Set(12345)

	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_processed_messages_db_rows" {
			found = true
			break
		}
	}

	if !found {
		t.Error("mailtagger_processed_messages_db_rows metric not found")
	}
}

func TestTickDurationSeconds(t *testing.T) {
	TickDurationSeconds.WithLabelValues().Observe(5.5)

	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range metrics {
		if mf.GetName() == "mailtagger_tick_duration_seconds" {
			found = true
			break
		}
	}

	if !found {
		t.Error("mailtagger_tick_duration_seconds metric not found")
	}
}
