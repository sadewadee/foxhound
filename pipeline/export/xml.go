package export

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"

	foxhound "github.com/sadewadee/foxhound"
)

// XMLWriter exports scraped items to an XML file.
// It implements the foxhound.Writer interface.
//
// The output format is:
//
//	<?xml version="1.0" encoding="UTF-8"?>
//	<rootElement>
//	  <itemElement>
//	    <fieldName>value</fieldName>
//	    ...
//	  </itemElement>
//	</rootElement>
type XMLWriter struct {
	file        *os.File
	enc         *xml.Encoder
	rootElement string
	itemElement string
	count       int
}

// NewXML opens path for writing and returns an XMLWriter.
// rootElement defaults to "items"; itemElement defaults to "item" when blank.
// Returns an error if the file cannot be created.
func NewXML(path, rootElement, itemElement string) (*XMLWriter, error) {
	if rootElement == "" {
		rootElement = "items"
	}
	if itemElement == "" {
		itemElement = "item"
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("export: opening XML output file %q: %w", path, err)
	}

	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")

	// Write XML declaration.
	if _, err := f.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("export: writing XML declaration: %w", err)
	}
	// Open root element.
	if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: rootElement}}); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("export: writing XML root element: %w", err)
	}

	return &XMLWriter{
		file:        f,
		enc:         enc,
		rootElement: rootElement,
		itemElement: itemElement,
	}, nil
}

// Write serialises item.Fields as an XML item element.
// Fields are emitted in sorted key order for determinism.
func (w *XMLWriter) Write(_ context.Context, item *foxhound.Item) error {
	keys := sortedKeys(item)

	// Open item element.
	itemStart := xml.StartElement{Name: xml.Name{Local: w.itemElement}}
	if err := w.enc.EncodeToken(itemStart); err != nil {
		return fmt.Errorf("export: writing XML item start element: %w", err)
	}

	// Write each field as a child element.
	for _, k := range keys {
		val := fieldStr(item, k)
		elem := xml.StartElement{Name: xml.Name{Local: k}}
		if err := w.enc.EncodeToken(elem); err != nil {
			return fmt.Errorf("export: writing XML field %q start: %w", k, err)
		}
		if err := w.enc.EncodeToken(xml.CharData(val)); err != nil {
			return fmt.Errorf("export: writing XML field %q value: %w", k, err)
		}
		if err := w.enc.EncodeToken(elem.End()); err != nil {
			return fmt.Errorf("export: writing XML field %q end: %w", k, err)
		}
	}

	// Close item element.
	if err := w.enc.EncodeToken(itemStart.End()); err != nil {
		return fmt.Errorf("export: writing XML item end element: %w", err)
	}

	w.count++
	return nil
}

// Flush flushes the encoder and syncs the file.
func (w *XMLWriter) Flush(_ context.Context) error {
	if err := w.enc.Flush(); err != nil {
		return fmt.Errorf("export: flushing XML encoder: %w", err)
	}
	return w.file.Sync()
}

// Close writes the closing root element, flushes, and closes the file.
func (w *XMLWriter) Close() error {
	// Close root element.
	if err := w.enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: w.rootElement}}); err != nil {
		_ = w.file.Close()
		return fmt.Errorf("export: writing XML root end element: %w", err)
	}
	if err := w.enc.Flush(); err != nil {
		_ = w.file.Close()
		return fmt.Errorf("export: flushing XML encoder on close: %w", err)
	}
	return w.file.Close()
}
