package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

func TestExtractTable_WithHeaders(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<thead><tr><th>Name</th><th>Age</th></tr></thead>
		<tbody>
			<tr><td>Alice</td><td>30</td></tr>
			<tr><td>Bob</td><td>25</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#t1")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}
	if tbl == nil {
		t.Fatal("ExtractTable returned nil")
	}
	if len(tbl.Headers) != 2 {
		t.Fatalf("headers: got %d, want 2", len(tbl.Headers))
	}
	if tbl.Headers[0] != "Name" || tbl.Headers[1] != "Age" {
		t.Errorf("headers: got %v, want [Name Age]", tbl.Headers)
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("rows: got %d, want 2", len(tbl.Rows))
	}
	if tbl.Rows[0][0] != "Alice" || tbl.Rows[0][1] != "30" {
		t.Errorf("row 0: got %v, want [Alice 30]", tbl.Rows[0])
	}
	if tbl.Rows[1][0] != "Bob" || tbl.Rows[1][1] != "25" {
		t.Errorf("row 1: got %v, want [Bob 25]", tbl.Rows[1])
	}
}

func TestExtractTable_WithoutHeaders(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<tr><td>Alice</td><td>30</td></tr>
		<tr><td>Bob</td><td>25</td></tr>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#t1")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}
	if tbl == nil {
		t.Fatal("ExtractTable returned nil")
	}
	if len(tbl.Headers) != 0 {
		t.Errorf("headers: got %v, want empty", tbl.Headers)
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("rows: got %d, want 2", len(tbl.Rows))
	}
	if tbl.Rows[0][0] != "Alice" || tbl.Rows[0][1] != "30" {
		t.Errorf("row 0: got %v, want [Alice 30]", tbl.Rows[0])
	}
}

func TestExtractTable_ColspanRowspan(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<thead><tr><th>A</th><th>B</th><th>C</th></tr></thead>
		<tbody>
			<tr><td rowspan="2">R</td><td>1</td><td>2</td></tr>
			<tr><td colspan="2">wide</td></tr>
			<tr><td>x</td><td>y</td><td>z</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#t1")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}
	if tbl == nil {
		t.Fatal("ExtractTable returned nil")
	}
	if len(tbl.Rows) != 3 {
		t.Fatalf("rows: got %d, want 3", len(tbl.Rows))
	}
	// Row 0: R, 1, 2
	if tbl.Rows[0][0] != "R" {
		t.Errorf("row0 col0: got %q, want %q", tbl.Rows[0][0], "R")
	}
	if tbl.Rows[0][1] != "1" {
		t.Errorf("row0 col1: got %q, want %q", tbl.Rows[0][1], "1")
	}
	// Row 1: R (from rowspan), wide, wide (from colspan)
	if tbl.Rows[1][0] != "R" {
		t.Errorf("row1 col0: got %q, want %q (rowspan fill)", tbl.Rows[1][0], "R")
	}
	if tbl.Rows[1][1] != "wide" {
		t.Errorf("row1 col1: got %q, want %q", tbl.Rows[1][1], "wide")
	}
	if tbl.Rows[1][2] != "wide" {
		t.Errorf("row1 col2: got %q, want %q (colspan fill)", tbl.Rows[1][2], "wide")
	}
	// Row 2: x, y, z
	if tbl.Rows[2][0] != "x" || tbl.Rows[2][1] != "y" || tbl.Rows[2][2] != "z" {
		t.Errorf("row2: got %v, want [x y z]", tbl.Rows[2])
	}
}

func TestExtractTable_EmptyCells(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<thead><tr><th>A</th><th>B</th></tr></thead>
		<tbody>
			<tr><td>val</td><td></td></tr>
			<tr><td></td><td>val2</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#t1")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}
	if tbl == nil {
		t.Fatal("ExtractTable returned nil")
	}
	if tbl.Rows[0][0] != "val" {
		t.Errorf("row0 col0: got %q, want %q", tbl.Rows[0][0], "val")
	}
	if tbl.Rows[0][1] != "" {
		t.Errorf("row0 col1: got %q, want empty string", tbl.Rows[0][1])
	}
	if tbl.Rows[1][0] != "" {
		t.Errorf("row1 col0: got %q, want empty string", tbl.Rows[1][0])
	}
	if tbl.Rows[1][1] != "val2" {
		t.Errorf("row1 col1: got %q, want %q", tbl.Rows[1][1], "val2")
	}
}

func TestExtractTables_MultipleTables(t *testing.T) {
	html := `<html><body>
	<table><tr><td>T1</td></tr></table>
	<table><tr><td>T2</td></tr></table>
	<table><tr><td>T3</td></tr></table>
	</body></html>`

	resp := newHTMLResponse(html)
	tables, err := parse.ExtractTables(resp)
	if err != nil {
		t.Fatalf("ExtractTables error: %v", err)
	}
	if len(tables) != 3 {
		t.Fatalf("tables: got %d, want 3", len(tables))
	}
	if tables[0].Rows[0][0] != "T1" {
		t.Errorf("table 0: got %q, want %q", tables[0].Rows[0][0], "T1")
	}
	if tables[1].Rows[0][0] != "T2" {
		t.Errorf("table 1: got %q, want %q", tables[1].Rows[0][0], "T2")
	}
	if tables[2].Rows[0][0] != "T3" {
		t.Errorf("table 2: got %q, want %q", tables[2].Rows[0][0], "T3")
	}
}

func TestTable_AsItems(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<thead><tr><th>Name</th><th>Price</th></tr></thead>
		<tbody>
			<tr><td>Widget</td><td>9.99</td></tr>
			<tr><td>Gadget</td><td>19.99</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#t1")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}

	items := tbl.AsItems()
	if len(items) != 2 {
		t.Fatalf("items: got %d, want 2", len(items))
	}
	if items[0].Fields["Name"] != "Widget" {
		t.Errorf("item 0 Name: got %v, want Widget", items[0].Fields["Name"])
	}
	if items[0].Fields["Price"] != "9.99" {
		t.Errorf("item 0 Price: got %v, want 9.99", items[0].Fields["Price"])
	}
	if items[1].Fields["Name"] != "Gadget" {
		t.Errorf("item 1 Name: got %v, want Gadget", items[1].Fields["Name"])
	}
	if items[0].URL != "http://example.com" {
		t.Errorf("item URL: got %q, want %q", items[0].URL, "http://example.com")
	}
	if items[0].Timestamp.IsZero() {
		t.Error("item Timestamp is zero")
	}
}

func TestTable_Column(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<thead><tr><th>Name</th><th>Age</th></tr></thead>
		<tbody>
			<tr><td>Alice</td><td>30</td></tr>
			<tr><td>Bob</td><td>25</td></tr>
			<tr><td>Carol</td><td>35</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, _ := parse.ExtractTable(resp, "#t1")

	ages := tbl.Column("Age")
	if len(ages) != 3 {
		t.Fatalf("Column Age: got %d values, want 3", len(ages))
	}
	if ages[0] != "30" || ages[1] != "25" || ages[2] != "35" {
		t.Errorf("Column Age: got %v, want [30 25 35]", ages)
	}

	missing := tbl.Column("Nonexistent")
	if missing != nil {
		t.Errorf("Column Nonexistent: got %v, want nil", missing)
	}
}

func TestTable_Cell(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<thead><tr><th>Name</th><th>Age</th></tr></thead>
		<tbody>
			<tr><td>Alice</td><td>30</td></tr>
			<tr><td>Bob</td><td>25</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, _ := parse.ExtractTable(resp, "#t1")

	val := tbl.Cell(0, "Name")
	if val != "Alice" {
		t.Errorf("Cell(0, Name): got %q, want %q", val, "Alice")
	}
	val = tbl.Cell(1, "Age")
	if val != "25" {
		t.Errorf("Cell(1, Age): got %q, want %q", val, "25")
	}

	// Out of bounds.
	val = tbl.Cell(99, "Name")
	if val != "" {
		t.Errorf("Cell(99, Name): got %q, want empty", val)
	}
	val = tbl.Cell(0, "Nonexistent")
	if val != "" {
		t.Errorf("Cell(0, Nonexistent): got %q, want empty", val)
	}
}

func TestExtractTable_NoMatch(t *testing.T) {
	html := `<html><body><p>No table here</p></body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#nonexistent")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}
	if tbl != nil {
		t.Errorf("expected nil table, got %+v", tbl)
	}
}

func TestExtractTable_Caption(t *testing.T) {
	html := `<html><body>
	<table id="t1">
		<caption>Sales Data</caption>
		<thead><tr><th>Product</th><th>Revenue</th></tr></thead>
		<tbody>
			<tr><td>Alpha</td><td>100</td></tr>
		</tbody>
	</table>
	</body></html>`

	resp := newHTMLResponse(html)
	tbl, err := parse.ExtractTable(resp, "#t1")
	if err != nil {
		t.Fatalf("ExtractTable error: %v", err)
	}
	if tbl == nil {
		t.Fatal("ExtractTable returned nil")
	}
	if tbl.Caption != "Sales Data" {
		t.Errorf("Caption: got %q, want %q", tbl.Caption, "Sales Data")
	}
}
