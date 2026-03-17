package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// WebhookWriter sends items as JSON POST requests to a webhook URL.
// Items are buffered and sent in batches when the buffer reaches batchSize,
// on an explicit Flush, or on Close.
//
// WebhookWriter is safe for concurrent use.
type WebhookWriter struct {
	url       string
	client    *http.Client
	authType  string // e.g. "Bearer", "Basic"
	authToken string
	batchSize int
	buffer    []*foxhound.Item
	mu        sync.Mutex
	retries   int
}

// WebhookOption is a functional option for NewWebhook.
type WebhookOption func(*WebhookWriter)

// WithBatchSize sets the number of items accumulated before an automatic flush.
// Defaults to 1 (flush after every item).
func WithBatchSize(n int) WebhookOption {
	return func(w *WebhookWriter) {
		if n > 0 {
			w.batchSize = n
		}
	}
}

// WithAuth sets the Authorization header sent with every request.
// authType is the scheme (e.g. "Bearer", "Basic") and token is the credential.
func WithAuth(authType, token string) WebhookOption {
	return func(w *WebhookWriter) {
		w.authType = authType
		w.authToken = token
	}
}

// WithRetry sets how many times a failed POST is retried before giving up.
func WithRetry(n int) WebhookOption {
	return func(w *WebhookWriter) {
		if n >= 0 {
			w.retries = n
		}
	}
}

// NewWebhook creates a WebhookWriter that POSTs items to url.
// Default batch size is 1; default retries is 0 (no retries).
func NewWebhook(url string, opts ...WebhookOption) *WebhookWriter {
	w := &WebhookWriter{
		url:       url,
		client:    &http.Client{Timeout: 30 * time.Second},
		batchSize: 1,
		retries:   0,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Write buffers item and flushes automatically when the buffer reaches batchSize.
func (w *WebhookWriter) Write(ctx context.Context, item *foxhound.Item) error {
	w.mu.Lock()
	w.buffer = append(w.buffer, item)
	shouldFlush := len(w.buffer) >= w.batchSize
	w.mu.Unlock()

	if shouldFlush {
		return w.Flush(ctx)
	}
	return nil
}

// Flush sends all buffered items as a single JSON array POST.
// Does nothing if the buffer is empty.
func (w *WebhookWriter) Flush(ctx context.Context) error {
	w.mu.Lock()
	if len(w.buffer) == 0 {
		w.mu.Unlock()
		return nil
	}
	batch := w.buffer
	w.buffer = nil
	w.mu.Unlock()

	return w.sendBatch(ctx, batch)
}

// Close flushes any remaining buffered items and releases resources.
func (w *WebhookWriter) Close() error {
	return w.Flush(context.Background())
}

// sendBatch serialises items as a JSON array and POSTs it to the webhook URL.
// Retries on non-2xx or transport errors up to w.retries times.
func (w *WebhookWriter) sendBatch(ctx context.Context, items []*foxhound.Item) error {
	fields := make([]map[string]any, len(items))
	for i, item := range items {
		fields[i] = item.Fields
	}

	body, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("webhook: marshalling batch: %w", err)
	}

	var lastErr error
	attempts := w.retries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			slog.Info("webhook: retrying POST",
				"attempt", attempt+1,
				"url", w.url,
				"previous_error", lastErr,
			)
		}
		lastErr = w.post(ctx, body)
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("webhook: POST failed after %d attempt(s): %w", attempts, lastErr)
}

// post performs a single HTTP POST.
func (w *WebhookWriter) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.authType != "" && w.authToken != "" {
		req.Header.Set("Authorization", w.authType+" "+w.authToken)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("non-2xx response: %d", resp.StatusCode)
	}
	return nil
}
