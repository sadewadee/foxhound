package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"
)

const poolSQLiteSchema = `
CREATE TABLE IF NOT EXISTS pool (
    url  TEXT PRIMARY KEY,
    added_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
`

type SQLitePool struct {
	db *sql.DB
}

func NewSQLitePool(dbPath string) (*SQLitePool, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("pool: sqlite open %q: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("pool: sqlite WAL: %w", err)
	}
	if _, err := db.Exec(poolSQLiteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("pool: sqlite schema: %w", err)
	}
	slog.Debug("pool: sqlite opened", "path", dbPath)
	return &SQLitePool{db: db}, nil
}

func (p *SQLitePool) Add(_ context.Context, url string) error {
	_, err := p.db.Exec(`INSERT OR IGNORE INTO pool (url) VALUES (?)`, url)
	if err != nil {
		return fmt.Errorf("pool: sqlite add: %w", err)
	}
	return nil
}

func (p *SQLitePool) AddBatch(_ context.Context, urls []string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("pool: sqlite batch begin: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO pool (url) VALUES (?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("pool: sqlite batch prepare: %w", err)
	}
	defer stmt.Close()
	for _, u := range urls {
		stmt.Exec(u)
	}
	return tx.Commit()
}

func (p *SQLitePool) Drain(_ context.Context) ([]string, error) {
	rows, err := p.db.Query(`SELECT url FROM pool ORDER BY added_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("pool: sqlite drain select: %w", err)
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("pool: sqlite drain scan: %w", err)
		}
		urls = append(urls, u)
	}

	if _, err := p.db.Exec(`DELETE FROM pool`); err != nil {
		return nil, fmt.Errorf("pool: sqlite drain delete: %w", err)
	}
	return urls, nil
}

func (p *SQLitePool) Len() int {
	var n int
	p.db.QueryRow(`SELECT COUNT(*) FROM pool`).Scan(&n)
	return n
}

func (p *SQLitePool) Close() error {
	return p.db.Close()
}
