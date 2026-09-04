package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLSessionRepository struct {
	conn *db.Conn
}

func NewSQLSessionRepository(conn *db.Conn) *SQLSessionRepository {
	return &SQLSessionRepository{conn: conn}
}

func (r *SQLSessionRepository) CreateSession(ctx context.Context, s *Session) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, device, ip, created_at, last_seen_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.TokenHash, nullStr(s.Device), nullStr(s.IP),
		db.TimeToString(s.CreatedAt), db.TimeToString(s.LastSeenAt), db.TimeToString(s.ExpiresAt),
		db.NullableTimeToString(s.RevokedAt),
	)
	if err != nil {
		return fmt.Errorf("creando sesión: %w", err)
	}
	return nil
}

func (r *SQLSessionRepository) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	row := r.conn.QueryRowContext(ctx, sessionSelectColumns+` WHERE token_hash = ?`, tokenHash)
	s, err := scanSession(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *SQLSessionRepository) ListSessionsForUser(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := r.conn.QueryContext(ctx, sessionSelectColumns+` WHERE user_id = ? ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando sesiones: %w", err)
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		s, err := scanSessionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SQLSessionRepository) TouchSession(ctx context.Context, id string, lastSeenAt time.Time) error {
	_, err := r.conn.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, db.TimeToString(lastSeenAt), id)
	if err != nil {
		return fmt.Errorf("actualizando last_seen_at: %w", err)
	}
	return nil
}

// RevokeSession exige coincidencia de userID en la propia cláusula WHERE:
// defensa en profundidad contra IDOR incluso si una capa superior olvidara
// comprobar la propiedad de la sesión (§198).
func (r *SQLSessionRepository) RevokeSession(ctx context.Context, id, userID string) error {
	res, err := r.conn.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		db.TimeToString(time.Now().UTC()), id, userID)
	if err != nil {
		return fmt.Errorf("revocando sesión: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *SQLSessionRepository) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.conn.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		db.TimeToString(time.Now().UTC()), userID)
	if err != nil {
		return fmt.Errorf("revocando sesiones: %w", err)
	}
	return nil
}

func (r *SQLSessionRepository) DeleteExpiredSessions(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, db.TimeToString(before))
	if err != nil {
		return 0, fmt.Errorf("purgando sesiones expiradas: %w", err)
	}
	return res.RowsAffected()
}

const sessionSelectColumns = `SELECT id, user_id, token_hash, device, ip, created_at, last_seen_at, expires_at, revoked_at FROM sessions`

func scanSession(row *sql.Row) (*Session, error) {
	return scanSessionRow(row)
}

func scanSessionRow(row rowScanner) (*Session, error) {
	var (
		s                                Session
		device, ip                       sql.NullString
		createdAt, lastSeenAt, expiresAt string
		revokedAt                        sql.NullString
	)
	if err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &device, &ip, &createdAt, &lastSeenAt, &expiresAt, &revokedAt); err != nil {
		return nil, err
	}
	if device.Valid {
		s.Device = device.String
	}
	if ip.Valid {
		s.IP = ip.String
	}
	var err error
	if s.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if s.LastSeenAt, err = db.StringToTime(lastSeenAt); err != nil {
		return nil, fmt.Errorf("parseando last_seen_at: %w", err)
	}
	if s.ExpiresAt, err = db.StringToTime(expiresAt); err != nil {
		return nil, fmt.Errorf("parseando expires_at: %w", err)
	}
	if s.RevokedAt, err = db.ParseNullableTime(revokedAt.String, revokedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando revoked_at: %w", err)
	}
	return &s, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
