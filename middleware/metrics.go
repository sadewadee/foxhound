package middleware

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metricsMiddleware emits Prometheus counters and histograms for every request.
type metricsMiddleware struct {
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	errors    *prometheus.CounterVec
}

// NewMetrics returns a Middleware that records Prometheus metrics.
//
// Three instruments are registered under namespace:
//
//   - <namespace>_requests_total{domain, status}     — request counter
//   - <namespace>_request_duration_seconds{domain}   — latency histogram
//   - <namespace>_errors_total{domain, error_type}   — error counter
//
// All instruments are registered with the default Prometheus registry via
// promauto, so they are automatically included in the default /metrics handler.
func NewMetrics(namespace string) foxhound.Middleware {
	requests := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests made, partitioned by domain and status code.",
		},
		[]string{"domain", "status"},
	)

	duration := promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Latency of HTTP requests in seconds, partitioned by domain.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"domain"},
	)

	errors := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "errors_total",
			Help:      "Total number of request errors, partitioned by domain and error type.",
		},
		[]string{"domain", "error_type"},
	)

	return &metricsMiddleware{
		requests: requests,
		duration: duration,
		errors:   errors,
	}
}

// Wrap returns a Fetcher that records metrics for each request/response pair.
func (m *metricsMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		domain := job.Domain
		if domain == "" {
			domain = "unknown"
		}

		start := time.Now()
		resp, err := next.Fetch(ctx, job)
		elapsed := time.Since(start)

		m.duration.WithLabelValues(domain).Observe(elapsed.Seconds())

		if err != nil {
			errType := errorType(err)
			m.errors.WithLabelValues(domain, errType).Inc()
			slog.Debug("metrics: error recorded", "domain", domain, "error_type", errType)
			// Still record a request with status "error".
			m.requests.WithLabelValues(domain, "error").Inc()
			return resp, err
		}

		status := "0"
		if resp != nil {
			status = strconv.Itoa(resp.StatusCode)
		}
		m.requests.WithLabelValues(domain, status).Inc()
		slog.Debug("metrics: request recorded",
			"domain", domain, "status", status, "duration", elapsed)
		return resp, nil
	})
}

// errorType classifies an error for the error_type label.
// It returns the first segment of the error message to avoid high cardinality.
func errorType(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case isContextError(err):
		return "context"
	default:
		return "fetch"
	}
}

// isContextError reports whether err is a context cancellation or timeout.
func isContextError(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
