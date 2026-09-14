package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLVersionRepository struct {
	conn *db.Conn
}

func NewSQLVersionRepository(conn *db.Conn) *SQLVersionRepository {
	return &SQLVersionRepository{conn: conn}
}

func (r *SQLVersionRepository) CreateVersion(ctx context.Context, v *FileVersion) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO file_versions (id, file_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.FileID, v.VersionNum, v.SizeBytes, v.SHA256, v.MimeType, v.StorageKey, db.TimeToString(v.CreatedAt))
	if err != nil {
		return fmt.Errorf("guardando versión: %w", err)
	}
	return nil
}

func (r *SQLVersionRepository) ListVersions(ctx context.Context, fileID string) ([]*FileVersion, error) {
	rows, err := r.conn.QueryContext(ctx,
		versionSelectColumns+` WHERE file_id = ? ORDER BY version_num DESC`, fileID)
	if err != nil {
		return nil, fmt.Errorf("listando versiones: %w", err)
	}
	defer rows.Close()

	var out []*FileVersion
	for rows.Next() {
		v, err := scanVersionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *SQLVersionRepository) GetVersion(ctx context.Context, fileID string, versionNum int) (*FileVersion, error) {
	row := r.conn.QueryRowContext(ctx,
		versionSelectColumns+` WHERE file_id = ? AND version_num = ?`, fileID, versionNum)
	v, err := scanVersionRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVersionNotFound
		}
		return nil, err
	}
	return v, nil
}

func (r *SQLVersionRepository) LatestVersionNum(ctx context.Context, fileID string) (int, error) {
	var n sql.NullInt64
	err := r.conn.QueryRowContext(ctx,
		`SELECT MAX(version_num) FROM file_versions WHERE file_id = ?`, fileID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("consultando última versión: %w", err)
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

func (r *SQLVersionRepository) DeleteVersion(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM file_versions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("eliminando versión: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrVersionNotFound
	}
	return nil
}

func (r *SQLVersionRepository) DeleteAllVersions(ctx context.Context, fileID string) error {
	_, err := r.conn.ExecContext(ctx, `DELETE FROM file_versions WHERE file_id = ?`, fileID)
	if err != nil {
		return fmt.Errorf("eliminando historial de versiones: %w", err)
	}
	return nil
}

const versionSelectColumns = `SELECT id, file_id, version_num, size_bytes, sha256, mime_type, storage_key, created_at FROM file_versions`

func scanVersionRow(row rowScanner) (*FileVersion, error) {
	var v FileVersion
	var createdAt string
	if err := row.Scan(&v.ID, &v.FileID, &v.VersionNum, &v.SizeBytes, &v.SHA256, &v.MimeType, &v.StorageKey, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if v.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	return &v, nil
}
