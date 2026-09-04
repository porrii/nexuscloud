package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLPoolRepository struct {
	conn *db.Conn
}

func NewSQLPoolRepository(conn *db.Conn) *SQLPoolRepository {
	return &SQLPoolRepository{conn: conn}
}

func (r *SQLPoolRepository) CreatePool(ctx context.Context, p *Pool) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO storage_pools (id, name, type, path, priority, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Type, p.Path, p.Priority, p.Status, db.TimeToString(p.CreatedAt))
	if err != nil {
		return fmt.Errorf("creando storage pool: %w", err)
	}
	return nil
}

func (r *SQLPoolRepository) GetPoolByID(ctx context.Context, id string) (*Pool, error) {
	row := r.conn.QueryRowContext(ctx, poolSelectColumns+` WHERE id = ?`, id)
	p, err := scanPool(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *SQLPoolRepository) ListPools(ctx context.Context) ([]*Pool, error) {
	rows, err := r.conn.QueryContext(ctx, poolSelectColumns+` ORDER BY priority, created_at`)
	if err != nil {
		return nil, fmt.Errorf("listando storage pools: %w", err)
	}
	defer rows.Close()

	var out []*Pool
	for rows.Next() {
		p, err := scanPoolRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLPoolRepository) DefaultPool(ctx context.Context) (*Pool, error) {
	row := r.conn.QueryRowContext(ctx, poolSelectColumns+` WHERE status = 'active' ORDER BY priority, created_at LIMIT 1`)
	p, err := scanPool(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	return p, nil
}

const poolSelectColumns = `SELECT id, name, type, path, priority, status, created_at FROM storage_pools`

func scanPool(row *sql.Row) (*Pool, error) { return scanPoolRow(row) }

func scanPoolRow(row rowScanner) (*Pool, error) {
	var p Pool
	var createdAt string
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &p.Path, &p.Priority, &p.Status, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if p.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	return &p, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}
