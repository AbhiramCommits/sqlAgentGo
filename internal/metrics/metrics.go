// Package metrics holds the Prometheus collectors for the conversion
// pipeline. Collectors are registered on the default registry via promauto
// and served by the /metrics endpoint of `sqlagent serve`.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ConversionAttempts records how many generation attempts a conversion
	// needed, labeled by final status.
	ConversionAttempts = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "conversion_attempts",
		Help:    "Generation attempts per conversion, by final status.",
		Buckets: []float64{1, 2, 3, 4, 5, 8, 12},
	}, []string{"status"})

	// ConversionsTotal counts finished conversions by final status.
	ConversionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "conversions_total",
		Help: "Total conversions, by final status.",
	}, []string{"status"})

	// GuardViolationsTotal counts groundedness violations by kind
	// (table, column, parse).
	GuardViolationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "guard_violations_total",
		Help: "Total groundedness-guard violations, by kind.",
	}, []string{"kind"})

	// LLMTokensTotal counts LLM tokens by direction (prompt, completion).
	LLMTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_tokens_total",
		Help: "Total LLM tokens, by type.",
	}, []string{"type"})

	// LLMRequestDuration tracks per-call latency of chat completions.
	LLMRequestDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "llm_request_duration_seconds",
		Help:    "LLM chat completion call duration in seconds.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	})
)
