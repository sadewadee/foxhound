package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; registers the "sqlite" driver name

	foxhound "github.com/sadewadee/foxhound"
)

// SQLiteQueue implements foxhound.Queue using SQLite for durability.
//
// Jobs survive process restarts, making this queue suitable for the
// resume capability described in the architecture docs.  Jobs are stored
// in a single table and moved from status='pending' to status='processing'
// on Pop so that concurrent callers each see a distinct job.
type SQLiteQueue struct {
	db *sql.DB
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS jobs (
    id         TEXT    PRIMARY KEY,
    data       TEXT    NOT NULL,
    priority   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT 0,
    status     TEXT    NOT NULL DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_jobs_pending ON jobs (priority DESC, created_at ASC)
    WHERE status = 'pending';
`

// NewSQLite opens (or creates) a SQLite database at dbPath and initialises
// the jobs table.
func NewSQLite(dbPath string) (*SQLiteQueue, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("queue: sqlite open %q: %w", dbPath, err)
	}
	// Serialised writes via a single connection are simplest and sufficient
	// for single-process use. WAL mode improves concurrent read performance.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("queue: sqlite WAL pragma: %w", err)
	}
	if _, err := db.Exec(sqliteSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("queue: sqlite schema: %w", err)
	}
	slog.Debug("queue: sqlite opened", "path", dbPath)
	return &SQLiteQueue{db: db}, nil
}

// Push serialises the job as JSON and inserts it into the jobs table.
// Duplicate IDs are rejected via the PRIMARY KEY constraint.
func (q *SQLiteQueue) Push(_ context.Context, job *foxhound.Job) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("queue: sqlite push: marshal: %w", err)
	}
	_, err = q.db.Exec(
		`INSERT INTO jobs (id, data, priority, created_at, status) VALUES (?, ?, ?, ?, 'pending')`,
		job.ID,
		string(data),
		int(job.Priority),
		job.CreatedAt.UnixMicro(),
	)
	if err != nil {
		return fmt.Errorf("queue: sqlite push: insert: %w", err)
	}
	slog.Debug("queue: sqlite pushed job", "id", job.ID, "priority", job.Priority)
	return nil
}

// Pop atomically selects the highest-priority pending job and marks it
// 'processing'.  It polls every 100 ms while the queue is empty, and
// returns when the context is cancelled.
func (q *SQLiteQueue) Pop(ctx context.Context) (*foxhound.Job, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		job, err := q.tryPop()
		if err != nil {
			return nil, err
		}
		if job != nil {
			return job, nil
		}

		// Queue empty — wait and retry.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// tryPop executes a single SELECT + UPDATE inside a transaction.
// Returns (nil, nil) when no pending jobs exist.
func (q *SQLiteQueue) tryPop() (*foxhound.Job, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("queue: sqlite pop: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var id, data string
	err = tx.QueryRow(
		`SELECT id, data FROM jobs
         WHERE status = 'pending'
         ORDER BY priority DESC, created_at ASC
         LIMIT 1`,
	).Scan(&id, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queue: sqlite pop: select: %w", err)
	}

	if _, err := tx.Exec(`UPDATE jobs SET status = 'processing' WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("queue: sqlite pop: update status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("queue: sqlite pop: commit: %w", err)
	}

	var job foxhound.Job
	if err := json.Unmarshal([]byte(data), &job); err != nil {
		return nil, fmt.Errorf("queue: sqlite pop: unmarshal: %w", err)
	}
	slog.Debug("queue: sqlite popped job", "id", job.ID, "priority", job.Priority)
	return &job, nil
}

// Len returns the number of pending jobs (status='pending').
func (q *SQLiteQueue) Len() int {
	var n int
	if err := q.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status = 'pending'`).Scan(&n); err != nil {
		slog.Warn("queue: sqlite len", "error", err)
		return 0
	}
	return n
}

// Close closes the underlying database connection.
func (q *SQLiteQueue) Close() error {
	return q.db.Close()
}
