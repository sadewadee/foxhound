package parse_test

import (
	"path/filepath"
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

func TestNewAdaptiveExtractorWithOptions_NoOptions_InMemory(t *testing.T) {
	ext := parse.NewAdaptiveExtractorWithOptions()
	if ext == nil {
		t.Fatal("expected extractor, got nil")
	}
	// Save with no storage configured should be a no-op.
	if err := ext.Save(); err != nil {
		t.Errorf("Save with no storage: %v", err)
	}
}

func TestNewAdaptiveExtractorWithOptions_JSONStorage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sigs.json")

	ext := parse.NewAdaptiveExtractorWithOptions(parse.WithJSONStorage(path))
	ext.Register("title", "h1.product-title")
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	_ = ext.Extract(doc, "title")
	if err := ext.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// New extractor pointed at same file should reload signatures and find
	// the element on a redesigned page via similarity fallback.
	ext2 := parse.NewAdaptiveExtractorWithOptions(parse.WithJSONStorage(path))
	redesigned := newAdaptiveDoc(t, adaptiveRedesignedHTML)
	if el := ext2.Extract(redesigned, "title"); el == nil {
		t.Error("expected fallback match after JSON storage reload, got nil")
	}
}

func TestNewAdaptiveExtractorWithOptions_SQLiteStorage(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sigs.db")

	ext := parse.NewAdaptiveExtractorWithOptions(parse.WithSQLiteStorage(dbPath))
	if ext == nil {
		t.Fatal("expected extractor, got nil")
	}
	ext.Register("title", "h1.product-title")
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	_ = ext.Extract(doc, "title")
	// Save should not error even though there is no JSON path: SQLite
	// path is configured.
	if err := ext.Save(); err != nil {
		t.Errorf("Save with SQLite: %v", err)
	}
}
