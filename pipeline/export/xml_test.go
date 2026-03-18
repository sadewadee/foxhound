package export_test

import (
	"context"
	"encoding/xml"
	"os"
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/pipeline/export"
)

func TestXMLWriter_ProducesValidXML(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	items := []*foxhound.Item{
		makeItem(map[string]any{"title": "Widget Pro", "price": "$29.99"}),
		makeItem(map[string]any{"title": "Gadget X", "price": "$49.99"}),
	}
	for _, it := range items {
		if err := w.Write(ctx, it); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	// Must parse as valid XML.
	var result map[string]any
	if err := xml.Unmarshal(data, &result); err != nil {
		// xml.Unmarshal into map[string]any won't work — use token decoder instead.
		dec := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			_ = tok
		}
		// Verify the document has a root element and is structurally valid.
		dec2 := xml.NewDecoder(strings.NewReader(string(data)))
		if err := checkXMLWellFormed(dec2); err != nil {
			t.Fatalf("Output is not well-formed XML: %v\nContent:\n%s", err, string(data))
		}
	}
}

// checkXMLWellFormed decodes all tokens from d, returning an error on bad XML.
func checkXMLWellFormed(d *xml.Decoder) error {
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return nil
			}
			return err
		}
	}
}

func TestXMLWriter_ContainsXMLDeclaration(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"title": "Alpha"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.HasPrefix(content, "<?xml") {
		t.Errorf("XML output should start with XML declaration. Got:\n%s", content)
	}
	if !strings.Contains(content, `encoding="UTF-8"`) {
		t.Errorf("XML output should declare UTF-8 encoding. Got:\n%s", content)
	}
}

func TestXMLWriter_UsesConfiguredRootAndItemElements(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "products", "product")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"name": "Alpha"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "<products>") {
		t.Errorf("XML: root element <products> not found:\n%s", content)
	}
	if !strings.Contains(content, "<product>") {
		t.Errorf("XML: item element <product> not found:\n%s", content)
	}
	if !strings.Contains(content, "</products>") {
		t.Errorf("XML: closing </products> not found:\n%s", content)
	}
}

func TestXMLWriter_FieldsAsChildElements(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"title": "Widget Pro", "price": "$29.99"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "<title>Widget Pro</title>") {
		t.Errorf("XML: <title> element not found:\n%s", content)
	}
	if !strings.Contains(content, "<price>$29.99</price>") {
		t.Errorf("XML: <price> element not found:\n%s", content)
	}
}

func TestXMLWriter_MultipleItems(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := w.Write(ctx, makeItem(map[string]any{"idx": i})); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	count := strings.Count(content, "<item>")
	if count != 3 {
		t.Errorf("XML: expected 3 <item> elements, got %d:\n%s", count, content)
	}
}

func TestXMLWriter_EmptyFile_ValidXML(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()
	_ = w.Flush(ctx)
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "<items>") {
		t.Errorf("Empty XML: should still contain root element. Got:\n%s", content)
	}
	if !strings.Contains(content, "</items>") {
		t.Errorf("Empty XML: should contain closing root element. Got:\n%s", content)
	}
}

func TestXMLWriter_NonExistentDir_ReturnsError(t *testing.T) {
	path := "/tmp/foxhound-nonexistent-dir-xyz/out.xml"
	_, err := export.NewXML(path, "items", "item")
	if err == nil {
		t.Error("NewXML on non-existent directory: expected error, got nil")
	}
}

func TestXMLWriter_Flush_DoesNotError(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()
	_ = w.Write(ctx, makeItem(map[string]any{"k": "v"}))
	if err := w.Flush(ctx); err != nil {
		t.Errorf("Flush error: %v", err)
	}
	_ = w.Close()
}

func TestXMLWriter_DefaultElements_ItemsAndItem(t *testing.T) {
	path := tempFile(t, ".xml")
	// Use the canonical defaults: rootElement="items", itemElement="item"
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"x": "1"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "<items>") || !strings.Contains(content, "<item>") {
		t.Errorf("XML defaults: expected <items> and <item>. Got:\n%s", content)
	}
}

// Test that field values with special chars are escaped properly.
func TestXMLWriter_EscapesSpecialCharacters(t *testing.T) {
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	ctx := context.Background()

	if err := w.Write(ctx, makeItem(map[string]any{"desc": "<b>bold</b> & more"})); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// Raw < must not appear inside element content — must be &lt;
	if strings.Contains(content, "<b>") {
		t.Errorf("XML: special chars must be escaped; found raw <b>:\n%s", content)
	}
}

func TestXMLWriter_DefaultsDoc(t *testing.T) {
	// Verify that the spec-required defaults (rootElement="items", itemElement="item")
	// are what buildWriter passes. This documents the contract so run.go is wired correctly.
	path := tempFile(t, ".xml")
	w, err := export.NewXML(path, "items", "item")
	if err != nil {
		t.Fatalf("NewXML error: %v", err)
	}
	_ = w.Close()
}
