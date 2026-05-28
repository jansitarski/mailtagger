// Package metrics provides Prometheus metrics for mailtagger observability.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "mailtagger"

// Message processing metrics
var (
	// MessagesProcessedTotal counts total messages processed by account and category.
	MessagesProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "messages_processed_total",
			Help:      "Total number of messages processed, labeled by account email and classification category.",
		},
		[]string{"account", "category"},
	)

	// MessagesSkippedTotal counts messages skipped (already processed or has AI label).
	MessagesSkippedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "messages_skipped_total",
			Help:      "Total number of messages skipped, labeled by account and reason.",
		},
		[]string{"account", "reason"},
	)
)

// LLM metrics
var (
	// LLMRequestDurationSeconds measures LLM request latency as a histogram.
	LLMRequestDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "llm_request_duration_seconds",
			Help:      "Histogram of LLM request duration in seconds.",
			Buckets:   []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
		},
		[]string{"provider", "model"},
	)

	// LLMErrorsTotal counts LLM errors by provider and reason.
	LLMErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "llm_errors_total",
			Help:      "Total number of LLM errors, labeled by provider and error reason.",
		},
		[]string{"provider", "reason"},
	)
)

// Gmail API metrics
var (
	// GmailAPIErrorsTotal counts Gmail API errors by operation and HTTP status code.
	GmailAPIErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "gmail_api_errors_total",
			Help:      "Total number of Gmail API errors, labeled by operation and HTTP status code.",
		},
		[]string{"op", "code"},
	)

	// GmailAPIRequestsTotal counts Gmail API requests by operation (for request rate).
	GmailAPIRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "gmail_api_requests_total",
			Help:      "Total number of Gmail API requests, labeled by operation.",
		},
		[]string{"op"},
	)
)

// Pipeline state metrics
var (
	// HistoryCursorAgeSeconds is a gauge for the age of the history cursor in seconds.
	// Set per account, computed as time.Now() - cursor_updated_at.
	HistoryCursorAgeSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "history_cursor_age_seconds",
			Help:      "Age of the Gmail history cursor in seconds, per account.",
		},
		[]string{"account"},
	)

	// ProcessedMessagesDBRows is a gauge for the number of rows in processed_messages table.
	ProcessedMessagesDBRows = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "processed_messages_db_rows",
			Help:      "Number of rows in the processed_messages table, per account.",
		},
		[]string{"account"},
	)
)

// Pipeline tick metrics
var (
	// TickDurationSeconds measures the duration of pipeline ticks.
	TickDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tick_duration_seconds",
			Help:      "Histogram of pipeline tick duration in seconds.",
			Buckets:   []float64{0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0},
		},
		[]string{}, // no labels, global tick timing
	)

	// TickMessagesProcessed tracks messages processed per tick.
	TickMessagesProcessed = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tick_messages_processed",
			Help:      "Histogram of messages processed per tick.",
			Buckets:   []float64{0, 1, 5, 10, 25, 50, 100, 250, 500},
		},
		[]string{},
	)
)
