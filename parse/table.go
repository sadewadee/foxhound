package parse

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// Table represents a parsed HTML table with resolved colspan/rowspan.
type Table struct {
	Headers []string   // column names from <th> or first row
	Rows    [][]string // data rows (rectangular grid)
	Caption string     // <caption> text if present
	url     string     // source URL for Item construction
}

// ExtractTable parses HTML from resp, finds the table matching selector, and
// returns a *Table with resolved colspan/rowspan. Returns nil, nil when no
// table matches.
func ExtractTable(resp *foxhound.Response, selector string) (*Table, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	sel := doc.Find(selector).First()
	if sel.Length() == 0 {
		return nil, nil
	}

	t := parseTable(sel)
	if t == nil {
		return nil, nil
	}
	t.url = resp.URL
	return t, nil
}

// ExtractTables finds ALL <table> elements on the page and returns each as
// a *Table.
func ExtractTables(resp *foxhound.Response) ([]*Table, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	var tables []*Table
	doc.Find("table").Each(func(_ int, sel *goquery.Selection) {
		t := parseTable(sel)
		if t != nil {
			t.url = resp.URL
			tables = append(tables, t)
		}
	})
	return tables, nil
}

// AsItems converts rows to Items using headers as field keys. Each row becomes
// one Item with Fields[header] = cellValue. URL and Timestamp are set on each
// Item.
func (t *Table) AsItems() []*foxhound.Item {
	if len(t.Headers) == 0 {
		return nil
	}

	items := make([]*foxhound.Item, 0, len(t.Rows))
	for _, row := range t.Rows {
		item := &foxhound.Item{
			Fields:    make(map[string]any, len(t.Headers)),
			Meta:      make(map[string]any),
			URL:       t.url,
			Timestamp: time.Now(),
		}
		for i, header := range t.Headers {
			if i < len(row) {
				item.Fields[header] = row[i]
			} else {
				item.Fields[header] = ""
			}
		}
		items = append(items, item)
	}
	return items
}

// Row returns the row at index i (zero-based). Returns nil if out of bounds.
func (t *Table) Row(i int) []string {
	if i < 0 || i >= len(t.Rows) {
		return nil
	}
	return t.Rows[i]
}

// Column returns all values for a named column. Returns nil if the column
// name is not found in Headers.
func (t *Table) Column(name string) []string {
	idx := -1
	for i, h := range t.Headers {
		if h == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}

	vals := make([]string, 0, len(t.Rows))
	for _, row := range t.Rows {
		if idx < len(row) {
			vals = append(vals, row[idx])
		} else {
			vals = append(vals, "")
		}
	}
	return vals
}

// Cell returns the value at the given row index and named column.
// Returns "" if the row or column is out of bounds.
func (t *Table) Cell(row int, col string) string {
	if row < 0 || row >= len(t.Rows) {
		return ""
	}
	idx := -1
	for i, h := range t.Headers {
		if h == col {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	if idx >= len(t.Rows[row]) {
		return ""
	}
	return t.Rows[row][idx]
}

// parseTable extracts a Table from a goquery Selection representing a <table>
// element. It uses a grid-fill algorithm to properly resolve colspan/rowspan.
func parseTable(sel *goquery.Selection) *Table {
	// 1. Extract caption.
	caption := strings.TrimSpace(sel.Find("caption").First().Text())

	// 2. Collect all <tr> rows.
	var trs []*goquery.Selection
	sel.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		trs = append(trs, tr)
	})
	if len(trs) == 0 {
		return nil
	}

	// 3. Pre-scan to find max columns (accounting for colspan).
	maxCols := 0
	for _, tr := range trs {
		cols := 0
		tr.Find("td, th").Each(func(_ int, cell *goquery.Selection) {
			cs := intAttr(cell, "colspan", 1)
			cols += cs
		})
		if cols > maxCols {
			maxCols = cols
		}
	}

	// 4. Build grid with phantom cell tracking.
	grid := make([][]string, len(trs))
	for i := range grid {
		grid[i] = make([]string, maxCols)
	}

	// Track which cells have been filled by rowspan so we skip them.
	filled := make([][]bool, len(trs))
	for i := range filled {
		filled[i] = make([]bool, maxCols)
	}

	for rowIdx, tr := range trs {
		colIdx := 0
		tr.Find("td, th").Each(func(_ int, cell *goquery.Selection) {
			// Skip cells already filled by previous rowspans.
			for colIdx < maxCols && filled[rowIdx][colIdx] {
				colIdx++
			}
			if colIdx >= maxCols {
				return
			}

			cs := intAttr(cell, "colspan", 1)
			rs := intAttr(cell, "rowspan", 1)
			text := strings.TrimSpace(cell.Text())

			// Fill the grid region.
			for r := rowIdx; r < rowIdx+rs && r < len(trs); r++ {
				for c := colIdx; c < colIdx+cs && c < maxCols; c++ {
					grid[r][c] = text
					filled[r][c] = true
				}
			}
			colIdx += cs
		})
	}

	// 5. Extract headers.
	var headers []string
	isHeaderRow := true
	trs[0].Find("td, th").Each(func(_ int, cell *goquery.Selection) {
		if cell.Is("td") {
			isHeaderRow = false
		}
	})

	// Check <thead> first.
	thead := sel.Find("thead")
	if thead.Length() > 0 {
		thead.Find("tr").First().Find("th, td").Each(func(_ int, cell *goquery.Selection) {
			text := strings.TrimSpace(cell.Text())
			cs := intAttr(cell, "colspan", 1)
			for c := 0; c < cs; c++ {
				headers = append(headers, text)
			}
		})
	} else if isHeaderRow {
		// First row is all <th> — use as headers.
		headers = make([]string, len(grid[0]))
		copy(headers, grid[0])
	}

	// 6. Separate data rows from header rows.
	dataStart := 0
	if len(headers) > 0 {
		dataStart = 1
		if thead.Length() > 0 {
			// thead rows don't count in tbody numbering — find where tbody starts.
			theadRows := thead.Find("tr").Length()
			if theadRows > 0 {
				dataStart = theadRows
			}
		}
	}

	rows := grid[dataStart:]

	return &Table{
		Headers: headers,
		Rows:    rows,
		Caption: caption,
	}
}

// intAttr reads an integer HTML attribute with a fallback default.
func intAttr(sel *goquery.Selection, name string, defaultVal int) int {
	val, exists := sel.Attr(name)
	if !exists {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 1 {
		return defaultVal
	}
	return n
}
