package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLAnonymousUploadRepository struct {
	conn *db.Conn
}

func NewSQLAnonymousUploadRepository(conn *db.Conn) *SQLAnonymousUploadRepository {
	return &SQLAnonymousUploadRepository{conn: conn}
}

func (r *SQLAnonymousUploadRepository) CreateLink(ctx context.Context, a *AnonymousUpload) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO anonymous_uploads (id, owner_id, directory_id, token_hash, label, max_upload_size_bytes, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.OwnerID, a.DirectoryID, a.TokenHash, a.Label,
		nullableInt64Arg(a.MaxUploadSizeBytes), db.NullableTimeToString(a.ExpiresAt), db.TimeToString(a.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("creando enlace de subida anónima: %w", err)
	}
	return nil
}

func (r *SQLAnonymousUploadRepository) GetLinkByTokenHash(ctx context.Context, tokenHash string) (*AnonymousUpload, error) {
	row := r.conn.QueryRowContext(ctx, anonymousUploadSelectColumns+` WHERE token_hash = ?`, tokenHash)
	a, err := scanAnonymousUpload(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAnonymousUploadNotFound
		}
		return nil, err
	}
	return a, nil
}

func (r *SQLAnonymousUploadRepository) ListLinksForOwner(ctx context.Context, ownerID string) ([]*AnonymousUpload, error) {
	rows, err := r.conn.QueryContext(ctx, anonymousUploadSelectColumns+` WHERE owner_id = ? ORDER BY created_at ASC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listando enlaces de subida anónima: %w", err)
	}
	defer rows.Close()

	var out []*AnonymousUpload
	for rows.Next() {
		a, err := scanAnonymousUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *SQLAnonymousUploadRepository) RevokeLink(ctx context.Context, id, ownerID string, revokedAt time.Time) error {
	res, err := r.conn.ExecContext(ctx,
		`UPDATE anonymous_uploads SET revoked_at = ? WHERE id = ? AND owner_id = ? AND revoked_at IS NULL`,
		db.TimeToString(revokedAt), id, ownerID)
	if err != nil {
		return fmt.Errorf("revocando enlace de subida anónima: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrAnonymousUploadNotFound
	}
	return nil
}

func (r *SQLAnonymousUploadRepository) IncrementUploadCount(ctx context.Context, id string) error {
	_, err := r.conn.ExecContext(ctx, `UPDATE anonymous_uploads SET upload_count = upload_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("actualizando upload_count: %w", err)
	}
	return nil
}

const anonymousUploadSelectColumns = `
	SELECT id, owner_id, directory_id, token_hash, label, max_upload_size_bytes, expires_at, revoked_at, upload_count, created_at
	FROM anonymous_uploads`

func scanAnonymousUpload(row rowScanner) (*AnonymousUpload, error) {
	var (
		a                    AnonymousUpload
		maxUploadSizeBytes   sql.NullInt64
		expiresAt, revokedAt sql.NullString
		createdAt            string
	)
	if err := row.Scan(&a.ID, &a.OwnerID, &a.DirectoryID, &a.TokenHash, &a.Label,
		&maxUploadSizeBytes, &expiresAt, &revokedAt, &a.UploadCount, &createdAt); err != nil {
		return nil, err
	}
	if maxUploadSizeBytes.Valid {
		v := maxUploadSizeBytes.Int64
		a.MaxUploadSizeBytes = &v
	}
	var err error
	if a.ExpiresAt, err = db.ParseNullableTime(expiresAt.String, expiresAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando expires_at: %w", err)
	}
	if a.RevokedAt, err = db.ParseNullableTime(revokedAt.String, revokedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando revoked_at: %w", err)
	}
	if a.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	return &a, nil
}
