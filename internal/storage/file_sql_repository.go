package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLFileRepository struct {
	conn *db.Conn
}

func NewSQLFileRepository(conn *db.Conn) *SQLFileRepository {
	return &SQLFileRepository{conn: conn}
}

func (r *SQLFileRepository) UpsertFile(ctx context.Context, meta *FileMeta) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO files (id, pool_id, owner_id, parent_path, name, size_bytes, sha256, mime_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pool_id, owner_id, parent_path, name)
		DO UPDATE SET size_bytes = excluded.size_bytes, sha256 = excluded.sha256,
			mime_type = excluded.mime_type, updated_at = excluded.updated_at`,
		meta.ID, meta.PoolID, meta.OwnerID, meta.ParentPath, meta.Name,
		meta.SizeBytes, meta.SHA256, meta.MimeType,
		db.TimeToString(meta.CreatedAt), db.TimeToString(meta.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("guardando metadatos de archivo: %w", err)
	}

	// El ON CONFLICT puede haber conservado el id/created_at de una fila
	// preexistente en vez de los que traía meta: releemos por clave natural
	// para que el llamador reciba siempre el estado real persistido.
	row := r.conn.QueryRowContext(ctx, fileSelectColumns+
		` WHERE pool_id = ? AND owner_id = ? AND parent_path = ? AND name = ?`,
		meta.PoolID, meta.OwnerID, meta.ParentPath, meta.Name)
	real, err := scanFile(row)
	if err != nil {
		return fmt.Errorf("releyendo archivo tras guardar: %w", err)
	}
	*meta = *real
	return nil
}

func (r *SQLFileRepository) GetFileByID(ctx context.Context, id string) (*FileMeta, error) {
	row := r.conn.QueryRowContext(ctx, fileSelectColumns+` WHERE id = ?`, id)
	f, err := scanFile(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return f, nil
}

func (r *SQLFileRepository) ListFiles(ctx context.Context, ownerID, parentPath string) ([]*FileMeta, error) {
	rows, err := r.conn.QueryContext(ctx,
		fileSelectColumns+` WHERE owner_id = ? AND parent_path = ? ORDER BY name`, ownerID, parentPath)
	if err != nil {
		return nil, fmt.Errorf("listando archivos: %w", err)
	}
	defer rows.Close()

	var out []*FileMeta
	for rows.Next() {
		f, err := scanFileRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *SQLFileRepository) DeleteFile(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("eliminando metadatos de archivo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrFileNotFound
	}
	return nil
}

const fileSelectColumns = `SELECT id, pool_id, owner_id, parent_path, name, size_bytes, sha256, mime_type, created_at, updated_at FROM files`

func scanFile(row *sql.Row) (*FileMeta, error) { return scanFileRow(row) }

func scanFileRow(row rowScanner) (*FileMeta, error) {
	var f FileMeta
	var createdAt, updatedAt string
	if err := row.Scan(&f.ID, &f.PoolID, &f.OwnerID, &f.ParentPath, &f.Name,
		&f.SizeBytes, &f.SHA256, &f.MimeType, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	if f.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if f.UpdatedAt, err = db.StringToTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parseando updated_at: %w", err)
	}
	return &f, nil
}
