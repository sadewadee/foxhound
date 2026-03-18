package export

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "github.com/lib/pq" // PostgreSQL driver

	foxhound "github.com/sadewadee/foxhound"
)

const defaultPGBatchSize = 100

// PostgresOption is a functional option for PostgresWriter.
type PostgresOption func(*PostgresWriter)

// WithUpsert configures the writer to use INSERT ... ON CONFLICT DO UPDATE
// keyed on the JSONB field keyField. When set, a unique index on
// data->>'<keyField>' is created automatically.
func WithUpsert(keyField string) PostgresOption {
	return func(w *PostgresWriter) {
		w.upsertKey = keyField
	}
}

// WithPGBatchSize sets how many items are buffered before an automatic flush.
// Values <= 0 are ignored and the default (100) is used.
func WithPGBatchSize(n int) PostgresOption {
	return func(w *PostgresWriter) {
		if n > 0 {
			w.batchSize = n
		}
	}
}

// PostgresWriter exports items to a PostgreSQL table as JSONB documents.
// Each item's Fields map is serialised to a JSONB column called data.
// The table is created automatically on first use.
//
// PostgresWriter is safe for concurrent use.
type PostgresWriter struct {
	db        *sql.DB
	table     string
	upsertKey string
	batchSize int
	buffer    []*foxhound.Item
	mu        sync.Mutex
}

// NewPostgres opens a connection to connString, ensures the target table exists,
// and returns a PostgresWriter ready for use.
//
// connString must be a lib/pq compatible connection string, e.g.:
//
//	"postgres://user:pass@localhost/mydb?sslmode=disable"
func NewPostgres(connString, table string, opts ...PostgresOption) (*PostgresWriter, error) {
	w := &PostgresWriter{
		table:     table,
		batchSize: defaultPGBatchSize,
	}
	for _, opt := range opts {
		opt(w)
	}

	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening connection: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: pinging database: %w", err)
	}
	w.db = db

	if err := w.ensureTable(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: ensuring table %q: %w", table, err)
	}

	return w, nil
}

// ensureTable creates the table and any required indexes if they do not exist.
func (w *PostgresWriter) ensureTable() error {
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id         SERIAL PRIMARY KEY,
			data       JSONB NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`, w.table)

	if _, err := w.db.Exec(createSQL); err != nil {
		return fmt.Errorf("creating table: %w", err)
	}

	if w.upsertKey != "" {
		// Create a unique index on the upsert key path inside the JSONB column.
		// Using IF NOT EXISTS avoids errors on repeated startups.
		indexName := fmt.Sprintf("uidx_%s_%s", w.table, w.upsertKey)
		indexSQL := fmt.Sprintf(
			`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s ((data->>'%s'))`,
			indexName, w.table, w.upsertKey,
		)
		if _, err := w.db.Exec(indexSQL); err != nil {
			return fmt.Errorf("creating unique index on %q: %w", w.upsertKey, err)
		}
	}

	return nil
}

// Write buffers item and flushes automatically when the buffer reaches batchSize.
func (w *PostgresWriter) Write(ctx context.Context, item *foxhound.Item) error {
	w.mu.Lock()
	w.buffer = append(w.buffer, item)
	shouldFlush := len(w.buffer) >= w.batchSize
	w.mu.Unlock()

	if shouldFlush {
		return w.Flush(ctx)
	}
	return nil
}

// Flush inserts all buffered items into Postgres in a single transaction.
// Does nothing if the buffer is empty.
func (w *PostgresWriter) Flush(ctx context.Context) error {
	w.mu.Lock()
	if len(w.buffer) == 0 {
		w.mu.Unlock()
		return nil
	}
	batch := w.buffer
	w.buffer = nil
	w.mu.Unlock()

	return w.insertBatch(ctx, batch)
}

// insertBatch writes all items in batch to the database inside a transaction.
func (w *PostgresWriter) insertBatch(ctx context.Context, items []*foxhound.Item) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: beginning transaction: %w", err)
	}

	var insertSQL string
	if w.upsertKey != "" {
		// ON CONFLICT on the JSONB key path: update data and refresh created_at.
		insertSQL = fmt.Sprintf(
			`INSERT INTO %s (data) VALUES ($1)
			 ON CONFLICT ((data->>'%s')) DO UPDATE SET data = EXCLUDED.data, created_at = NOW()`,
			w.table, w.upsertKey,
		)
	} else {
		insertSQL = fmt.Sprintf(`INSERT INTO %s (data) VALUES ($1)`, w.table)
	}

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("postgres: preparing insert: %w", err)
	}
	defer stmt.Close()

	for _, item := range items {
		data, err := json.Marshal(item.Fields)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("postgres: marshalling item fields: %w", err)
		}
		if _, err := stmt.ExecContext(ctx, data); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("postgres: inserting item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres: committing transaction: %w", err)
	}
	return nil
}

// Close flushes any remaining buffered items and closes the database connection.
func (w *PostgresWriter) Close() error {
	if err := w.Flush(context.Background()); err != nil {
		_ = w.db.Close()
		return err
	}
	return w.db.Close()
}
