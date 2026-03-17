package export_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/pipeline/export"
)

// captureServer records all request bodies sent to it.
type captureServer struct {
	mu       sync.Mutex
	bodies   [][]byte
	statuses []int
	server   *httptest.Server
}

func newCaptureServer(statusCode int) *captureServer {
	cs := &captureServer{}
	cs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cs.mu.Lock()
		cs.bodies = append(cs.bodies, body)
		cs.statuses = append(cs.statuses, statusCode)
		cs.mu.Unlock()
		w.WriteHeader(statusCode)
	}))
	return cs
}

func (cs *captureServer) URL() string { return cs.server.URL }
func (cs *captureServer) Close()      { cs.server.Close() }

func (cs *captureServer) RequestCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.bodies)
}

func (cs *captureServer) LastBody() []byte {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.bodies) == 0 {
		return nil
	}
	return cs.bodies[len(cs.bodies)-1]
}

func webhookItem(url, title string) *foxhound.Item {
	item := foxhound.NewItem()
	item.URL = url
	item.Set("title", title)
	return item
}

func TestWebhookWriter_SingleItemFlushedOnClose(t *testing.T) {
	srv := newCaptureServer(http.StatusOK)
	defer srv.Close()

	w := export.NewWebhook(srv.URL())
	ctx := context.Background()

	if err := w.Write(ctx, webhookItem("http://a.com", "A")); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 request, got %d", srv.RequestCount())
	}

	var items []map[string]any
	if err := json.Unmarshal(srv.LastBody(), &items); err != nil {
		t.Fatalf("response JSON parse error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item in payload, got %d", len(items))
	}
}

func TestWebhookWriter_BatchSizeTriggersFlush(t *testing.T) {
	srv := newCaptureServer(http.StatusOK)
	defer srv.Close()

	w := export.NewWebhook(srv.URL(), export.WithBatchSize(3))
	ctx := context.Background()

	// Write 3 items — should auto-flush when batch is full
	for i := 0; i < 3; i++ {
		if err := w.Write(ctx, webhookItem("http://example.com", "item")); err != nil {
			t.Fatalf("Write %d error: %v", i, err)
		}
	}

	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 request after batch full, got %d", srv.RequestCount())
	}

	var items []map[string]any
	if err := json.Unmarshal(srv.LastBody(), &items); err != nil {
		t.Fatalf("batch body parse error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected batch of 3, got %d", len(items))
	}
}

func TestWebhookWriter_BatchSizePartialFlushOnClose(t *testing.T) {
	srv := newCaptureServer(http.StatusOK)
	defer srv.Close()

	w := export.NewWebhook(srv.URL(), export.WithBatchSize(5))
	ctx := context.Background()

	// Write 2 items (less than batch size)
	_ = w.Write(ctx, webhookItem("http://a.com", "A"))
	_ = w.Write(ctx, webhookItem("http://b.com", "B"))

	if srv.RequestCount() != 0 {
		t.Errorf("expected no flush before Close, got %d requests", srv.RequestCount())
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 flush on Close, got %d", srv.RequestCount())
	}
}

func TestWebhookWriter_MultipleBatches(t *testing.T) {
	srv := newCaptureServer(http.StatusOK)
	defer srv.Close()

	w := export.NewWebhook(srv.URL(), export.WithBatchSize(2))
	ctx := context.Background()

	// Write 4 items: 2 batches of 2
	for i := 0; i < 4; i++ {
		if err := w.Write(ctx, webhookItem("http://example.com", "item")); err != nil {
			t.Fatalf("Write %d error: %v", i, err)
		}
	}

	if srv.RequestCount() != 2 {
		t.Errorf("expected 2 batches, got %d", srv.RequestCount())
	}
}

func TestWebhookWriter_WithAuth_BearerHeader(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := export.NewWebhook(srv.URL, export.WithAuth("Bearer", "mytoken123"))
	ctx := context.Background()

	_ = w.Write(ctx, webhookItem("http://a.com", "A"))
	_ = w.Close()

	if receivedAuth != "Bearer mytoken123" {
		t.Errorf("Authorization header: got %q, want %q", receivedAuth, "Bearer mytoken123")
	}
}

func TestWebhookWriter_RetriesOnFailure(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := export.NewWebhook(srv.URL, export.WithRetry(3))
	ctx := context.Background()

	_ = w.Write(ctx, webhookItem("http://a.com", "A"))
	if err := w.Close(); err != nil {
		t.Fatalf("Close with retry error: %v", err)
	}

	if callCount < 3 {
		t.Errorf("expected at least 3 calls (with retries), got %d", callCount)
	}
}

func TestWebhookWriter_FlushEmptyBuffer_NoRequest(t *testing.T) {
	srv := newCaptureServer(http.StatusOK)
	defer srv.Close()

	w := export.NewWebhook(srv.URL())
	ctx := context.Background()

	// Flush with nothing buffered should not send a request
	if err := w.Flush(ctx); err != nil {
		t.Fatalf("Flush empty error: %v", err)
	}

	if srv.RequestCount() != 0 {
		t.Errorf("expected 0 requests for empty flush, got %d", srv.RequestCount())
	}
}

func TestWebhookWriter_ContentTypeIsJSON(t *testing.T) {
	var receivedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := export.NewWebhook(srv.URL)
	ctx := context.Background()

	_ = w.Write(ctx, webhookItem("http://a.com", "A"))
	_ = w.Close()

	if receivedContentType != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", receivedContentType)
	}
}
