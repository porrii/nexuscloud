package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLShareRepository struct {
	conn *db.Conn
}

func NewSQLShareRepository(conn *db.Conn) *SQLShareRepository {
	return &SQLShareRepository{conn: conn}
}

// nullableStr convierte "" (convención de este dominio para "sin valor", ver
// share.go) en NULL de SQL -- necesario para que los índices parciales
// (`WHERE target_user_id IS NOT NULL`, etc.) y el CHECK de file_id/
// directory_id se comporten correctamente: una cadena vacía NO es NULL.
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableIntArg(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt64Arg(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func (r *SQLShareRepository) CreateShare(ctx context.Context, s *Share) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO shares (
			id, owner_id, file_id, directory_id, share_type,
			target_user_id, target_group_id, token_hash, label,
			can_download, can_upload, password_hash,
			expires_at, max_downloads, download_count, max_upload_size_bytes,
			revoked_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.OwnerID, nullableStr(s.FileID), nullableStr(s.DirectoryID), string(s.Type),
		nullableStr(s.TargetUserID), nullableStr(s.TargetGroupID), nullableStr(s.TokenHash), s.Label,
		s.CanDownload, s.CanUpload, nullableStr(s.PasswordHash),
		db.NullableTimeToString(s.ExpiresAt), nullableIntArg(s.MaxDownloads), s.DownloadCount, nullableInt64Arg(s.MaxUploadSizeBytes),
		db.NullableTimeToString(s.RevokedAt), db.TimeToString(s.CreatedAt), db.TimeToString(s.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("guardando share: %w", err)
	}
	return nil
}

const shareSelectColumns = `SELECT
	id, owner_id, file_id, directory_id, share_type,
	target_user_id, target_group_id, token_hash, label,
	can_download, can_upload, password_hash,
	expires_at, max_downloads, download_count, max_upload_size_bytes,
	revoked_at, created_at, updated_at
	FROM shares`

func scanShareRow(row rowScanner) (*Share, error) {
	var s Share
	var shareType string
	var fileID, directoryID, targetUserID, targetGroupID, tokenHash, passwordHash sql.NullString
	var expiresAt, revokedAt sql.NullString
	var maxDownloads, maxUploadSizeBytes sql.NullInt64
	var createdAt, updatedAt string

	err := row.Scan(
		&s.ID, &s.OwnerID, &fileID, &directoryID, &shareType,
		&targetUserID, &targetGroupID, &tokenHash, &s.Label,
		&s.CanDownload, &s.CanUpload, &passwordHash,
		&expiresAt, &maxDownloads, &s.DownloadCount, &maxUploadSizeBytes,
		&revokedAt, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	s.Type = ShareType(shareType)
	s.FileID = fileID.String
	s.DirectoryID = directoryID.String
	s.TargetUserID = targetUserID.String
	s.TargetGroupID = targetGroupID.String
	s.TokenHash = tokenHash.String
	s.PasswordHash = passwordHash.String

	if maxDownloads.Valid {
		v := int(maxDownloads.Int64)
		s.MaxDownloads = &v
	}
	if maxUploadSizeBytes.Valid {
		v := maxUploadSizeBytes.Int64
		s.MaxUploadSizeBytes = &v
	}

	if s.ExpiresAt, err = db.ParseNullableTime(expiresAt.String, expiresAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando expires_at: %w", err)
	}
	if s.RevokedAt, err = db.ParseNullableTime(revokedAt.String, revokedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando revoked_at: %w", err)
	}
	if s.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if s.UpdatedAt, err = db.StringToTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parseando updated_at: %w", err)
	}
	return &s, nil
}

func (r *SQLShareRepository) GetShareByID(ctx context.Context, id string) (*Share, error) {
	row := r.conn.QueryRowContext(ctx, shareSelectColumns+` WHERE id = ?`, id)
	s, err := scanShareRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrShareNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *SQLShareRepository) GetShareByTokenHash(ctx context.Context, tokenHash string) (*Share, error) {
	row := r.conn.QueryRowContext(ctx, shareSelectColumns+` WHERE token_hash = ?`, tokenHash)
	s, err := scanShareRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrShareNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *SQLShareRepository) ListSharesByOwner(ctx context.Context, ownerID string) ([]*Share, error) {
	rows, err := r.conn.QueryContext(ctx,
		shareSelectColumns+` WHERE owner_id = ? AND revoked_at IS NULL ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("listando shares del propietario: %w", err)
	}
	defer rows.Close()
	return scanShareRows(rows)
}

// ListSharesForUser junta shares dirigidos directamente a userID con shares
// dirigidos a cualquier grupo del que userID sea miembro (JOIN con
// user_groups vía SQL crudo -- este paquete no importa internal/users, igual
// que files/directories ya referencian users(id) sin importar ese paquete).
func (r *SQLShareRepository) ListSharesForUser(ctx context.Context, userID string, now time.Time) ([]*Share, error) {
	rows, err := r.conn.QueryContext(ctx, shareSelectColumns+`
		WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)
		AND (
			(share_type = 'user' AND target_user_id = ?)
			OR (share_type = 'group' AND target_group_id IN (
				SELECT group_id FROM user_groups WHERE user_id = ?
			))
		)
		ORDER BY created_at DESC`,
		db.TimeToString(now), userID, userID)
	if err != nil {
		return nil, fmt.Errorf("listando shares recibidos: %w", err)
	}
	defer rows.Close()
	return scanShareRows(rows)
}

func scanShareRows(rows *sql.Rows) ([]*Share, error) {
	var out []*Share
	for rows.Next() {
		s, err := scanShareRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// hasAccessQuery es idéntica para archivo/carpeta salvo la columna de
// recurso comparada -- dos constantes estáticas en vez de interpolar el
// nombre de columna en la consulta, para que ninguna parte del SQL dependa
// de una cadena construida en tiempo de ejecución (§168 defensa en
// profundidad), aunque aquí el valor nunca vendría de entrada de usuario.
const (
	hasFileAccessQuery = `SELECT 1 FROM shares
		WHERE file_id = ? AND can_download = 1
		AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)
		AND (
			(share_type = 'user' AND target_user_id = ?)
			OR (share_type = 'group' AND target_group_id IN (
				SELECT group_id FROM user_groups WHERE user_id = ?
			))
		)
		LIMIT 1`
	hasDirectoryAccessQuery = `SELECT 1 FROM shares
		WHERE directory_id = ? AND can_download = 1
		AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)
		AND (
			(share_type = 'user' AND target_user_id = ?)
			OR (share_type = 'group' AND target_group_id IN (
				SELECT group_id FROM user_groups WHERE user_id = ?
			))
		)
		LIMIT 1`
)

func (r *SQLShareRepository) HasFileAccess(ctx context.Context, userID, fileID string, now time.Time) (bool, error) {
	return r.hasAccess(ctx, hasFileAccessQuery, userID, fileID, now)
}

func (r *SQLShareRepository) HasDirectoryAccess(ctx context.Context, userID, directoryID string, now time.Time) (bool, error) {
	return r.hasAccess(ctx, hasDirectoryAccessQuery, userID, directoryID, now)
}

func (r *SQLShareRepository) hasAccess(ctx context.Context, query, userID, resourceID string, now time.Time) (bool, error) {
	var one int
	err := r.conn.QueryRowContext(ctx, query, resourceID, db.TimeToString(now), userID, userID).Scan(&one)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("comprobando acceso vía share: %w", err)
	}
	return true, nil
}

// IncrementDownloadCount incrementa de forma atómica (UPDATE...WHERE +
// RowsAffected, nunca leer-luego-escribir) solo si max_downloads todavía no
// se alcanzó -- protege el último hueco disponible frente a descargas
// concurrentes contra el mismo enlace.
func (r *SQLShareRepository) IncrementDownloadCount(ctx context.Context, shareID string) (bool, error) {
	res, err := r.conn.ExecContext(ctx, `
		UPDATE shares SET download_count = download_count + 1
		WHERE id = ? AND (max_downloads IS NULL OR download_count < max_downloads)`,
		shareID)
	if err != nil {
		return false, fmt.Errorf("incrementando contador de descargas: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *SQLShareRepository) RevokeShare(ctx context.Context, id string, revokedAt time.Time) error {
	res, err := r.conn.ExecContext(ctx,
		`UPDATE shares SET revoked_at = ?, updated_at = ? WHERE id = ? AND revoked_at IS NULL`,
		db.TimeToString(revokedAt), db.TimeToString(revokedAt), id)
	if err != nil {
		return fmt.Errorf("revocando share: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrShareNotFound
	}
	return nil
}
