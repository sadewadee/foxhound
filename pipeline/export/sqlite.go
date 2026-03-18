package export

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
	_ "modernc.org/sqlite" // SQLite driver
)

// SQLiteWriter exports scraped items to a SQLite database.
// It implements the foxhound.Writer interface.
//
// The table is created automatically from the first item's fields.
// Subsequent items that contain new fields trigger ALTER TABLE ADD COLUMN
// so the schema grows incrementally.
type SQLiteWriter struct {
	db      *sql.DB
	table   string
	columns map[string]struct{} // columns that exist in the db table
}

// NewSQLite opens (or creates) the SQLite database at dbPath and returns a
// SQLiteWriter targeting the given table. Returns an error if the database
// cannot be opened.
func NewSQLite(dbPath, table string) (*SQLiteWriter, error) {
	if table == "" {
		table = "items"
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("export: opening SQLite database %q: %w", dbPath, err)
	}
	// Verify connectivity.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("export: connecting to SQLite database %q: %w", dbPath, err)
	}

	// Enable WAL for better write concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("export: setting WAL journal mode: %w", err)
	}

	return &SQLiteWriter{
		db:      db,
		table:   table,
		columns: make(map[string]struct{}),
	}, nil
}

// Write inserts item.Fields as a row into the SQLite table.
// On the first Write the table is created with TEXT columns for each field.
// For subsequent items, any new fields trigger ALTER TABLE ADD COLUMN.
func (w *SQLiteWriter) Write(ctx context.Context, item *foxhound.Item) error {
	keys := sortedKeys(item)
	if len(keys) == 0 {
		return nil
	}

	// Ensure all columns exist.
	if err := w.ensureColumns(ctx, keys); err != nil {
		return err
	}

	// Build parameterised INSERT.
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, k := range keys {
		placeholders[i] = "?"
		args[i] = fieldStr(item, k)
	}

	quotedCols := make([]string, len(keys))
	for i, k := range keys {
		quotedCols[i] = `"` + strings.ReplaceAll(k, `"`, `""`) + `"`
	}

	query := fmt.Sprintf(
		"INSERT INTO %q (%s) VALUES (%s)",
		w.table,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)
	if _, err := w.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("export: inserting SQLite row: %w", err)
	}
	return nil
}

// ensureColumns creates the table if it does not exist and adds any missing
// columns via ALTER TABLE ADD COLUMN.
func (w *SQLiteWriter) ensureColumns(ctx context.Context, keys []string) error {
	if len(w.columns) == 0 {
		// First call — create the table.
		colDefs := make([]string, len(keys))
		for i, k := range keys {
			colDefs[i] = `"` + strings.ReplaceAll(k, `"`, `""`) + `" TEXT`
		}
		ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %q (%s)", w.table, strings.Join(colDefs, ", "))
		if _, err := w.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("export: creating SQLite table %q: %w", w.table, err)
		}
		for _, k := range keys {
			w.columns[k] = struct{}{}
		}
		return nil
	}

	// Subsequent calls — add any new columns.
	for _, k := range keys {
		if _, exists := w.columns[k]; exists {
			continue
		}
		quotedCol := `"` + strings.ReplaceAll(k, `"`, `""`) + `"`
		alter := fmt.Sprintf("ALTER TABLE %q ADD COLUMN %s TEXT", w.table, quotedCol)
		if _, err := w.db.ExecContext(ctx, alter); err != nil {
			return fmt.Errorf("export: adding column %q to SQLite table %q: %w", k, w.table, err)
		}
		w.columns[k] = struct{}{}
	}
	return nil
}

// Flush is a no-op for SQLite (each Write is immediately committed).
func (w *SQLiteWriter) Flush(_ context.Context) error {
	return nil
}

// Close closes the underlying database connection.
func (w *SQLiteWriter) Close() error {
	return w.db.Close()
}
