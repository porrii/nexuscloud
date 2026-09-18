package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

var (
	ErrWebAuthnCredentialNotFound = errors.New("auth: credencial WebAuthn no encontrada")
	ErrWebAuthnCeremonyNotFound   = errors.New("auth: ceremonia WebAuthn no encontrada o caducada")
)

// WebAuthnCredential es un passkey registrado (§25, ADR-033). CredentialID
// es el identificador que da el navegador/authenticator (base64url) -- ID
// es el identificador interno (idgen.New()), igual que en el resto de
// tablas del proyecto; nunca al revés (ver el comentario de la migración
// 0008 sobre por qué el credential ID externo no es la PK).
type WebAuthnCredential struct {
	ID           string
	UserID       string
	CredentialID string
	PublicKey    string
	SignCount    uint32
	Label        string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

type WebAuthnCredentialRepository interface {
	CreateCredential(ctx context.Context, c *WebAuthnCredential) error
	GetCredentialByCredentialID(ctx context.Context, credentialID string) (*WebAuthnCredential, error)
	ListCredentialsForUser(ctx context.Context, userID string) ([]*WebAuthnCredential, error)
	// UpdateSignCount persiste el nuevo sign_count/last_used_at tras un login
	// válido: es la propia protección contra replay que exige el estándar
	// WebAuthn, así que esta escritura no es opcional (ver ejemplos oficiales
	// de go-webauthn/webauthn: el credential devuelto por Finish siempre se
	// vuelve a guardar).
	UpdateSignCount(ctx context.Context, id string, signCount uint32, lastUsedAt time.Time) error
	// DeleteCredential exige coincidencia de userID en la propia cláusula
	// WHERE: defensa en profundidad contra IDOR (§198), mismo criterio que
	// SQLSessionRepository.RevokeSession.
	DeleteCredential(ctx context.Context, id, userID string) error
}

// WebAuthnCeremony es el estado efímero de un registro o login en curso
// (segundos/minutos, nunca horas) -- ver el comentario de la migración
// 0009 sobre por qué no reutiliza la tabla sessions. UserID va vacío en un
// login discoverable: el servidor todavía no sabe qué usuario está
// entrando hasta que el propio ceremony lo resuelve.
type WebAuthnCeremony struct {
	ID          string
	UserID      string
	Purpose     string
	SessionData string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

const (
	WebAuthnCeremonyPurposeRegistration = "registration"
	WebAuthnCeremonyPurposeLogin        = "login"
)

type WebAuthnCeremonyRepository interface {
	CreateCeremony(ctx context.Context, c *WebAuthnCeremony) error
	GetCeremony(ctx context.Context, id string) (*WebAuthnCeremony, error)
	DeleteCeremony(ctx context.Context, id string) error
	DeleteExpiredCeremonies(ctx context.Context, before time.Time) (int64, error)
}

type SQLWebAuthnCredentialRepository struct {
	conn *db.Conn
}

func NewSQLWebAuthnCredentialRepository(conn *db.Conn) *SQLWebAuthnCredentialRepository {
	return &SQLWebAuthnCredentialRepository{conn: conn}
}

func (r *SQLWebAuthnCredentialRepository) CreateCredential(ctx context.Context, c *WebAuthnCredential) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO webauthn_credentials (id, user_id, credential_id, public_key, sign_count, label, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.UserID, c.CredentialID, c.PublicKey, c.SignCount, c.Label,
		db.TimeToString(c.CreatedAt), db.NullableTimeToString(c.LastUsedAt),
	)
	if err != nil {
		return fmt.Errorf("creando credencial WebAuthn: %w", err)
	}
	return nil
}

func (r *SQLWebAuthnCredentialRepository) GetCredentialByCredentialID(ctx context.Context, credentialID string) (*WebAuthnCredential, error) {
	row := r.conn.QueryRowContext(ctx, webAuthnCredentialSelectColumns+` WHERE credential_id = ?`, credentialID)
	c, err := scanWebAuthnCredential(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWebAuthnCredentialNotFound
		}
		return nil, err
	}
	return c, nil
}

func (r *SQLWebAuthnCredentialRepository) ListCredentialsForUser(ctx context.Context, userID string) ([]*WebAuthnCredential, error) {
	rows, err := r.conn.QueryContext(ctx, webAuthnCredentialSelectColumns+` WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando credenciales WebAuthn: %w", err)
	}
	defer rows.Close()

	var out []*WebAuthnCredential
	for rows.Next() {
		c, err := scanWebAuthnCredentialRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *SQLWebAuthnCredentialRepository) UpdateSignCount(ctx context.Context, id string, signCount uint32, lastUsedAt time.Time) error {
	_, err := r.conn.ExecContext(ctx,
		`UPDATE webauthn_credentials SET sign_count = ?, last_used_at = ? WHERE id = ?`,
		signCount, db.TimeToString(lastUsedAt), id)
	if err != nil {
		return fmt.Errorf("actualizando sign_count: %w", err)
	}
	return nil
}

func (r *SQLWebAuthnCredentialRepository) DeleteCredential(ctx context.Context, id, userID string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("borrando credencial WebAuthn: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrWebAuthnCredentialNotFound
	}
	return nil
}

const webAuthnCredentialSelectColumns = `SELECT id, user_id, credential_id, public_key, sign_count, label, created_at, last_used_at FROM webauthn_credentials`

func scanWebAuthnCredential(row *sql.Row) (*WebAuthnCredential, error) {
	return scanWebAuthnCredentialRow(row)
}

func scanWebAuthnCredentialRow(row rowScanner) (*WebAuthnCredential, error) {
	var (
		c          WebAuthnCredential
		createdAt  string
		lastUsedAt sql.NullString
	)
	if err := row.Scan(&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.SignCount, &c.Label, &createdAt, &lastUsedAt); err != nil {
		return nil, err
	}
	var err error
	if c.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if c.LastUsedAt, err = db.ParseNullableTime(lastUsedAt.String, lastUsedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando last_used_at: %w", err)
	}
	return &c, nil
}

type SQLWebAuthnCeremonyRepository struct {
	conn *db.Conn
}

func NewSQLWebAuthnCeremonyRepository(conn *db.Conn) *SQLWebAuthnCeremonyRepository {
	return &SQLWebAuthnCeremonyRepository{conn: conn}
}

func (r *SQLWebAuthnCeremonyRepository) CreateCeremony(ctx context.Context, c *WebAuthnCeremony) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO webauthn_ceremonies (id, user_id, purpose, session_data, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, nullStr(c.UserID), c.Purpose, c.SessionData,
		db.TimeToString(c.CreatedAt), db.TimeToString(c.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("creando ceremonia WebAuthn: %w", err)
	}
	return nil
}

func (r *SQLWebAuthnCeremonyRepository) GetCeremony(ctx context.Context, id string) (*WebAuthnCeremony, error) {
	row := r.conn.QueryRowContext(ctx,
		`SELECT id, user_id, purpose, session_data, created_at, expires_at FROM webauthn_ceremonies WHERE id = ?`, id)

	var (
		c                  WebAuthnCeremony
		userID             sql.NullString
		createdAt, expires string
	)
	if err := row.Scan(&c.ID, &userID, &c.Purpose, &c.SessionData, &createdAt, &expires); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrWebAuthnCeremonyNotFound
		}
		return nil, err
	}
	if userID.Valid {
		c.UserID = userID.String
	}
	var err error
	if c.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if c.ExpiresAt, err = db.StringToTime(expires); err != nil {
		return nil, fmt.Errorf("parseando expires_at: %w", err)
	}
	return &c, nil
}

func (r *SQLWebAuthnCeremonyRepository) DeleteCeremony(ctx context.Context, id string) error {
	_, err := r.conn.ExecContext(ctx, `DELETE FROM webauthn_ceremonies WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("borrando ceremonia WebAuthn: %w", err)
	}
	return nil
}

func (r *SQLWebAuthnCeremonyRepository) DeleteExpiredCeremonies(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM webauthn_ceremonies WHERE expires_at < ?`, db.TimeToString(before))
	if err != nil {
		return 0, fmt.Errorf("purgando ceremonias WebAuthn caducadas: %w", err)
	}
	return res.RowsAffected()
}
