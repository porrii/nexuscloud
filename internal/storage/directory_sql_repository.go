package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLDirectoryRepository struct {
	conn *db.Conn
}

func NewSQLDirectoryRepository(conn *db.Conn) *SQLDirectoryRepository {
	return &SQLDirectoryRepository{conn: conn}
}

// CreateDirectory es idempotente (como os.MkdirAll) para carpetas activas.
// Si ya existe una fila para esa clave natural (activa o en papelera), no
// inserta una segunda -- el llamador (FileService.Mkdir) es responsable de
// comprobar antes si el nombre está ocupado por algo en la papelera.
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

func (r *SQLDirectoryRepository) GetDirectoryByNaturalKey(ctx context.Context, poolID, ownerID, parentPath, name string) (*Directory, error) {
	row := r.conn.QueryRowContext(ctx,
		directorySelectColumns+` WHERE pool_id = ? AND owner_id = ? AND parent_path = ? AND name = ?`,
		poolID, ownerID, parentPath, name)
	d, err := scanDirectoryRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDirectoryNotFound
		}
		return nil, err
	}
	return d, nil
}

func (r *SQLDirectoryRepository) ListDirectories(ctx context.Context, ownerID, parentPath string) ([]*Directory, error) {
	rows, err := r.conn.QueryContext(ctx,
		directorySelectColumns+` WHERE owner_id = ? AND parent_path = ? AND deleted_at IS NULL ORDER BY name`, ownerID, parentPath)
	if err != nil {
		return nil, fmt.Errorf("listando carpetas: %w", err)
	}
	defer rows.Close()
	return scanDirectoryRows(rows)
}

func (r *SQLDirectoryRepository) ListTrashedDirectories(ctx context.Context, ownerID string) ([]*Directory, error) {
	rows, err := r.conn.QueryContext(ctx,
		directorySelectColumns+` WHERE owner_id = ? AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listando papelera de carpetas: %w", err)
	}
	defer rows.Close()
	return scanDirectoryRows(rows)
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

func (r *SQLDirectoryRepository) SoftDeleteDirectory(ctx context.Context, id string, deletedAt time.Time) error {
	return r.setDeletedAt(ctx, id, db.TimeToString(deletedAt))
}

func (r *SQLDirectoryRepository) RestoreDirectory(ctx context.Context, id string) error {
	return r.setDeletedAt(ctx, id, nil)
}

func (r *SQLDirectoryRepository) setDeletedAt(ctx context.Context, id string, deletedAt any) error {
	res, err := r.conn.ExecContext(ctx, `UPDATE directories SET deleted_at = ? WHERE id = ?`, deletedAt, id)
	if err != nil {
		return fmt.Errorf("actualizando estado de papelera: %w", err)
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

func (r *SQLDirectoryRepository) ListDirectoriesDeletedBefore(ctx context.Context, cutoff time.Time) ([]*Directory, error) {
	rows, err := r.conn.QueryContext(ctx,
		directorySelectColumns+` WHERE deleted_at IS NOT NULL AND deleted_at < ?`, db.TimeToString(cutoff))
	if err != nil {
		return nil, fmt.Errorf("listando carpetas para purgar: %w", err)
	}
	defer rows.Close()
	return scanDirectoryRows(rows)
}

const directorySelectColumns = `SELECT id, pool_id, owner_id, parent_path, name, created_at, deleted_at FROM directories`

func scanDirectoryRows(rows *sql.Rows) ([]*Directory, error) {
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

func scanDirectoryRow(row rowScanner) (*Directory, error) {
	var d Directory
	var createdAt string
	var deletedAt sql.NullString
	if err := row.Scan(&d.ID, &d.PoolID, &d.OwnerID, &d.ParentPath, &d.Name, &createdAt, &deletedAt); err != nil {
		return nil, err
	}
	var err error
	if d.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if d.DeletedAt, err = db.ParseNullableTime(deletedAt.String, deletedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando deleted_at: %w", err)
	}
	return &d, nil
}
