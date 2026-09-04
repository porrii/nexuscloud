package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLDirectoryRepository struct {
	conn *db.Conn
}

func NewSQLDirectoryRepository(conn *db.Conn) *SQLDirectoryRepository {
	return &SQLDirectoryRepository{conn: conn}
}

// CreateDirectory es idempotente (como os.MkdirAll): crear una carpeta que
// ya existe no es un error.
func (r *SQLDirectoryRepository) CreateDirectory(ctx context.Context, d *Directory) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO directories (id, pool_id, owner_id, parent_path, name, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (pool_id, owner_id, parent_path, name) DO NOTHING`,
		d.ID, d.PoolID, d.OwnerID, d.ParentPath, d.Name, db.TimeToString(d.CreatedAt))
	if err != nil {
		return fmt.Errorf("creando carpeta: %w", err)
	}
	return nil
}

func (r *SQLDirectoryRepository) ListDirectories(ctx context.Context, ownerID, parentPath string) ([]*Directory, error) {
	rows, err := r.conn.QueryContext(ctx,
		directorySelectColumns+` WHERE owner_id = ? AND parent_path = ? ORDER BY name`, ownerID, parentPath)
	if err != nil {
		return nil, fmt.Errorf("listando carpetas: %w", err)
	}
	defer rows.Close()

	var out []*Directory
	for rows.Next() {
		d, err := scanDirectoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *SQLDirectoryRepository) GetDirectoryByID(ctx context.Context, id string) (*Directory, error) {
	row := r.conn.QueryRowContext(ctx, directorySelectColumns+` WHERE id = ?`, id)
	d, err := scanDirectoryRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDirectoryNotFound
		}
		return nil, err
	}
	return d, nil
}

func (r *SQLDirectoryRepository) DeleteDirectory(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM directories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("eliminando carpeta: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrDirectoryNotFound
	}
	return nil
}

const directorySelectColumns = `SELECT id, pool_id, owner_id, parent_path, name, created_at FROM directories`

func scanDirectoryRow(row rowScanner) (*Directory, error) {
	var d Directory
	var createdAt string
	if err := row.Scan(&d.ID, &d.PoolID, &d.OwnerID, &d.ParentPath, &d.Name, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if d.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	return &d, nil
}
