package engine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/lib/pq"
)

type PostgresPool struct {
	db    *sql.DB
	table string
}

func NewPostgresPool(dsn, table string) (*PostgresPool, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("pool: postgres open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pool: postgres ping: %w", err)
	}

	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			url      TEXT PRIMARY KEY,
			added_at TIMESTAMPTZ DEFAULT NOW()
		)`, table)
	if _, err := db.Exec(createSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("pool: postgres schema: %w", err)
	}

	slog.Debug("pool: postgres opened", "table", table)
	return &PostgresPool{db: db, table: table}, nil
}

func (p *PostgresPool) Add(_ context.Context, url string) error {
	_, err := p.db.Exec(
		fmt.Sprintf(`INSERT INTO %s (url) VALUES ($1) ON CONFLICT DO NOTHING`, p.table), url)
	if err != nil {
		return fmt.Errorf("pool: postgres add: %w", err)
	}
	return nil
}

func (p *PostgresPool) AddBatch(_ context.Context, urls []string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("pool: postgres batch begin: %w", err)
	}
	stmt, err := tx.Prepare(
		fmt.Sprintf(`INSERT INTO %s (url) VALUES ($1) ON CONFLICT DO NOTHING`, p.table))
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("pool: postgres batch prepare: %w", err)
	}
	defer stmt.Close()
	for _, u := range urls {
		stmt.Exec(u)
	}
	return tx.Commit()
}

func (p *PostgresPool) Drain(_ context.Context) ([]string, error) {
	rows, err := p.db.Query(
		fmt.Sprintf(`DELETE FROM %s RETURNING url`, p.table))
	if err != nil {
		return nil, fmt.Errorf("pool: postgres drain: %w", err)
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("pool: postgres drain scan: %w", err)
		}
		urls = append(urls, u)
	}
	return urls, nil
}

func (p *PostgresPool) Len() int {
	var n int
	p.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, p.table)).Scan(&n)
	return n
}

func (p *PostgresPool) Close() error {
	return p.db.Close()
}
