package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLAPITokenRepository struct {
	conn *db.Conn
}

func NewSQLAPITokenRepository(conn *db.Conn) *SQLAPITokenRepository {
	return &SQLAPITokenRepository{conn: conn}
}

func (r *SQLAPITokenRepository) CreateToken(ctx context.Context, t *APIToken) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, token_hash, label, created_at, expires_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.TokenHash, t.Label,
		db.TimeToString(t.CreatedAt), db.NullableTimeToString(t.ExpiresAt), db.NullableTimeToString(t.LastUsedAt),
	)
	if err != nil {
		return fmt.Errorf("creando token de API: %w", err)
	}
	return nil
}

func (r *SQLAPITokenRepository) GetTokenByHash(ctx context.Context, tokenHash string) (*APIToken, error) {
	row := r.conn.QueryRowContext(ctx, apiTokenSelectColumns+` WHERE token_hash = ?`, tokenHash)
	t, err := scanAPIToken(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAPITokenNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *SQLAPITokenRepository) ListTokensForUser(ctx context.Context, userID string) ([]*APIToken, error) {
	rows, err := r.conn.QueryContext(ctx, apiTokenSelectColumns+` WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando tokens de API: %w", err)
	}
	defer rows.Close()

	var out []*APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *SQLAPITokenRepository) TouchToken(ctx context.Context, id string, at time.Time) error {
	_, err := r.conn.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, db.TimeToString(at), id)
	if err != nil {
		return fmt.Errorf("actualizando last_used_at del token de API: %w", err)
	}
	return nil
}

// RevokeToken hace DELETE (no soft-delete): mismo criterio que
// webdav.TokenRepository.RevokeToken, no el de sessions -- aquí no hace
// falta re-consultar un token ya revocado.
func (r *SQLAPITokenRepository) RevokeToken(ctx context.Context, id, userID string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("revocando token de API: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

const apiTokenSelectColumns = `SELECT id, user_id, token_hash, label, created_at, expires_at, last_used_at FROM api_tokens`

func scanAPIToken(row rowScanner) (*APIToken, error) {
	var (
		t                   APIToken
		createdAt           string
		expiresAt, lastUsed sql.NullString
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Label, &createdAt, &expiresAt, &lastUsed); err != nil {
		return nil, err
	}
	var err error
	if t.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if t.ExpiresAt, err = db.ParseNullableTime(expiresAt.String, expiresAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando expires_at: %w", err)
	}
	if t.LastUsedAt, err = db.ParseNullableTime(lastUsed.String, lastUsed.Valid); err != nil {
		return nil, fmt.Errorf("parseando last_used_at: %w", err)
	}
	return &t, nil
}
