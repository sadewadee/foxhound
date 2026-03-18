package parse

import (
	"path/filepath"
	"testing"
)

func TestSQLiteAdaptiveStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_adaptive.db")

	store, err := NewSQLiteAdaptiveStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAdaptiveStore: %v", err)
	}
	defer store.Close()

	sig := &ElementSignature{
		Tag:       "div",
		ID:        "product-123",
		Classes:   []string{"card", "product"},
		Text:      "Widget",
		ParentTag: "section",
		Depth:     3,
		Position:  2,
	}

	// Save signature.
	if err := store.Save("example.com", "product_card", sig); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load it back.
	loaded, err := store.Load("example.com", "product_card")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil, expected a signature")
	}

	// Verify fields.
	if loaded.Tag != "div" {
		t.Errorf("Tag = %q, want div", loaded.Tag)
	}
	if loaded.ID != "product-123" {
		t.Errorf("ID = %q, want product-123", loaded.ID)
	}
	if len(loaded.Classes) != 2 || loaded.Classes[0] != "card" {
		t.Errorf("Classes = %v, want [card product]", loaded.Classes)
	}
	if loaded.Text != "Widget" {
		t.Errorf("Text = %q, want Widget", loaded.Text)
	}
}

func TestSQLiteAdaptiveStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_adaptive.db")

	store, err := NewSQLiteAdaptiveStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAdaptiveStore: %v", err)
	}
	defer store.Close()

	loaded, err := store.Load("example.com", "nonexistent")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded != nil {
		t.Errorf("Load returned %v, want nil for nonexistent key", loaded)
	}
}

func TestSQLiteAdaptiveStore_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_adaptive.db")

	store, err := NewSQLiteAdaptiveStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAdaptiveStore: %v", err)
	}
	defer store.Close()

	sig1 := &ElementSignature{Tag: "div", Text: "first"}
	sig2 := &ElementSignature{Tag: "span", Text: "second"}

	store.Save("example.com", "selector1", sig1)
	store.Save("example.com", "selector1", sig2) // overwrite

	loaded, _ := store.Load("example.com", "selector1")
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Tag != "span" || loaded.Text != "second" {
		t.Errorf("expected overwritten values, got Tag=%q Text=%q", loaded.Tag, loaded.Text)
	}
}

func TestSQLiteAdaptiveStore_DomainIsolation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_adaptive.db")

	store, err := NewSQLiteAdaptiveStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAdaptiveStore: %v", err)
	}
	defer store.Close()

	sigA := &ElementSignature{Tag: "div", Text: "domain-a"}
	sigB := &ElementSignature{Tag: "span", Text: "domain-b"}

	store.Save("site-a.com", "price", sigA)
	store.Save("site-b.com", "price", sigB)

	loadedA, _ := store.Load("site-a.com", "price")
	loadedB, _ := store.Load("site-b.com", "price")

	if loadedA.Text != "domain-a" {
		t.Errorf("site-a got %q, want domain-a", loadedA.Text)
	}
	if loadedB.Text != "domain-b" {
		t.Errorf("site-b got %q, want domain-b", loadedB.Text)
	}
}

func TestSQLiteAdaptiveStore_DomainNormalization(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_adaptive.db")

	store, err := NewSQLiteAdaptiveStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAdaptiveStore: %v", err)
	}
	defer store.Close()

	sig := &ElementSignature{Tag: "div"}
	store.Save("Example.COM", "test", sig)

	// Should find with different casing.
	loaded, _ := store.Load("example.com", "test")
	if loaded == nil {
		t.Error("domain normalization failed: could not load with lowercase domain")
	}
}

func TestHashIdentifier(t *testing.T) {
	h1 := hashIdentifier("selector_a")
	h2 := hashIdentifier("selector_b")
	h3 := hashIdentifier("selector_a") // same as h1

	if h1 == h2 {
		t.Error("different identifiers should produce different hashes")
	}
	if h1 != h3 {
		t.Error("same identifier should produce same hash")
	}
}

func TestFileAdaptiveStore_Interface(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adaptive.json")

	// Verify FileAdaptiveStore implements AdaptiveStore interface.
	var store AdaptiveStore = NewFileAdaptiveStore(path)
	defer store.Close()

	sig := &ElementSignature{Tag: "p", Text: "hello"}
	if err := store.Save("example.com", "greeting", sig); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("example.com", "greeting")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Tag != "p" {
		t.Errorf("Tag = %q, want p", loaded.Tag)
	}
}
