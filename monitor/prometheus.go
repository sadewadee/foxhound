package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusExporter exposes Foxhound metrics on a Prometheus-compatible HTTP
// endpoint. It uses its own isolated registry so it does not pollute the
// default global registry.
type PrometheusExporter struct {
	registry *prometheus.Registry
	server   *http.Server

	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	errorsTotal      *prometheus.CounterVec
	itemsTotal       prometheus.Counter
	bytesTotal       prometheus.Counter
	blockedTotal     prometheus.Counter
	escalationsTotal prometheus.Counter
	activeWalkers    prometheus.Gauge
	queueSize        prometheus.Gauge
}

// NewPrometheus creates a PrometheusExporter using the given metric namespace and
// HTTP port. Call Start to begin serving metrics.
func NewPrometheus(namespace string, port int) *PrometheusExporter {
	reg := prometheus.NewRegistry()

	p := &PrometheusExporter{
		registry: reg,
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total HTTP requests made, partitioned by domain and status code.",
		}, []string{"domain", "status"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency, partitioned by domain.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"domain"}),

		errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "errors_total",
			Help:      "Total errors, partitioned by domain and error type.",
		}, []string{"domain", "error_type"}),

		itemsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "items_total",
			Help:      "Total scraped items emitted by the pipeline.",
		}),

		bytesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "bytes_total",
			Help:      "Total response bytes downloaded.",
		}),

		blockedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "blocked_total",
			Help:      "Total requests that were blocked by the target site.",
		}),

		escalationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "escalations_total",
			Help:      "Total static→browser escalations performed by the smart router.",
		}),

		activeWalkers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_walkers",
			Help:      "Number of currently active walker goroutines.",
		}),

		queueSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "queue_size",
			Help:      "Current number of jobs waiting in the queue.",
		}),
	}

	reg.MustRegister(
		p.requestsTotal,
		p.requestDuration,
		p.errorsTotal,
		p.itemsTotal,
		p.bytesTotal,
		p.blockedTotal,
		p.escalationsTotal,
		p.activeWalkers,
		p.queueSize,
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	p.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return p
}

// Start begins serving the Prometheus metrics endpoint in a background goroutine.
// Returns an error only if the server fails to bind (i.e. port already in use).
func (p *PrometheusExporter) Start() error {
	slog.Info("prometheus: starting metrics server", "addr", p.server.Addr)
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("prometheus: metrics server error", "error", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the metrics HTTP server.
func (p *PrometheusExporter) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.server.Shutdown(ctx)
}

// RecordRequest records a completed HTTP request with its domain, status code,
// and duration.
func (p *PrometheusExporter) RecordRequest(domain string, status int, duration time.Duration) {
	statusStr := strconv.Itoa(status)
	p.requestsTotal.WithLabelValues(domain, statusStr).Inc()
	p.requestDuration.WithLabelValues(domain).Observe(duration.Seconds())
}

// RecordError records an error keyed by domain and error type label.
func (p *PrometheusExporter) RecordError(domain string, errType string) {
	p.errorsTotal.WithLabelValues(domain, errType).Inc()
}

// RecordItems adds n to the items counter.
func (p *PrometheusExporter) RecordItems(n int) {
	p.itemsTotal.Add(float64(n))
}

// RecordBlocked increments the blocked requests counter.
func (p *PrometheusExporter) RecordBlocked() {
	p.blockedTotal.Inc()
}

// RecordEscalation increments the static→browser escalation counter.
func (p *PrometheusExporter) RecordEscalation() {
	p.escalationsTotal.Inc()
}

// SetActiveWalkers sets the active walkers gauge to n.
func (p *PrometheusExporter) SetActiveWalkers(n int) {
	p.activeWalkers.Set(float64(n))
}

// SetQueueSize sets the queue size gauge to n.
func (p *PrometheusExporter) SetQueueSize(n int) {
	p.queueSize.Set(float64(n))
}
