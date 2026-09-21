package webdav

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLTokenRepository struct {
	conn *db.Conn
}

func NewSQLTokenRepository(conn *db.Conn) *SQLTokenRepository {
	return &SQLTokenRepository{conn: conn}
}

func (r *SQLTokenRepository) CreateToken(ctx context.Context, t *Token) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO webdav_tokens (id, user_id, token_hash, label, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.TokenHash, t.Label,
		db.TimeToString(t.CreatedAt), db.NullableTimeToString(t.LastUsedAt),
	)
	if err != nil {
		return fmt.Errorf("creando token WebDAV: %w", err)
	}
	return nil
}

func (r *SQLTokenRepository) GetTokenByHash(ctx context.Context, tokenHash string) (*Token, error) {
	row := r.conn.QueryRowContext(ctx, tokenSelectColumns+` WHERE token_hash = ?`, tokenHash)
	t, err := scanToken(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *SQLTokenRepository) ListTokensForUser(ctx context.Context, userID string) ([]*Token, error) {
	rows, err := r.conn.QueryContext(ctx, tokenSelectColumns+` WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando tokens WebDAV: %w", err)
	}
	defer rows.Close()

	var out []*Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *SQLTokenRepository) TouchToken(ctx context.Context, id string, at time.Time) error {
	_, err := r.conn.ExecContext(ctx, `UPDATE webdav_tokens SET last_used_at = ? WHERE id = ?`, db.TimeToString(at), id)
	if err != nil {
		return fmt.Errorf("actualizando last_used_at del token: %w", err)
	}
	return nil
}

func (r *SQLTokenRepository) RevokeToken(ctx context.Context, id, userID string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM webdav_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("revocando token WebDAV: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

const tokenSelectColumns = `SELECT id, user_id, token_hash, label, created_at, last_used_at FROM webdav_tokens`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanToken(row rowScanner) (*Token, error) {
	var (
		t          Token
		createdAt  string
		lastUsedAt sql.NullString
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Label, &createdAt, &lastUsedAt); err != nil {
		return nil, err
	}
	var err error
	if t.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if t.LastUsedAt, err = db.ParseNullableTime(lastUsedAt.String, lastUsedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando last_used_at: %w", err)
	}
	return &t, nil
}
