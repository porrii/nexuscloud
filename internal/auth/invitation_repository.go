package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLInvitationRepository struct {
	conn *db.Conn
}

func NewSQLInvitationRepository(conn *db.Conn) *SQLInvitationRepository {
	return &SQLInvitationRepository{conn: conn}
}

func (r *SQLInvitationRepository) CreateInvitation(ctx context.Context, inv *Invitation) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO invitations (id, token_hash, created_by, role_id, max_uses, use_count, expires_at, created_at, used_at, used_by, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, inv.TokenHash, inv.CreatedBy, nullStr(inv.RoleID), inv.MaxUses, inv.UseCount,
		db.TimeToString(inv.ExpiresAt), db.TimeToString(inv.CreatedAt),
		db.NullableTimeToString(inv.UsedAt), nullStr(inv.UsedBy), db.NullableTimeToString(inv.RevokedAt),
	)
	if err != nil {
		return fmt.Errorf("creando invitación: %w", err)
	}
	return nil
}

func (r *SQLInvitationRepository) GetInvitationByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error) {
	row := r.conn.QueryRowContext(ctx, invitationSelectColumns+` WHERE token_hash = ?`, tokenHash)
	inv, err := scanInvitation(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrInvitationNotFound
		}
		return nil, err
	}
	return inv, nil
}

func (r *SQLInvitationRepository) ListInvitations(ctx context.Context) ([]*Invitation, error) {
	rows, err := r.conn.QueryContext(ctx, invitationSelectColumns+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listando invitaciones: %w", err)
	}
	defer rows.Close()

	var out []*Invitation
	for rows.Next() {
		inv, err := scanInvitationRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (r *SQLInvitationRepository) RecordUse(ctx context.Context, id, usedByUserID string, usedAt time.Time) error {
	_, err := r.conn.ExecContext(ctx, `
		UPDATE invitations SET use_count = use_count + 1, used_at = ?, used_by = ? WHERE id = ?`,
		db.TimeToString(usedAt), usedByUserID, id)
	if err != nil {
		return fmt.Errorf("registrando uso de invitación: %w", err)
	}
	return nil
}

func (r *SQLInvitationRepository) RevokeInvitation(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx,
		`UPDATE invitations SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		db.TimeToString(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("revocando invitación: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvitationNotFound
	}
	return nil
}

const invitationSelectColumns = `SELECT id, token_hash, created_by, role_id, max_uses, use_count, expires_at, created_at, used_at, used_by, revoked_at FROM invitations`

func scanInvitation(row *sql.Row) (*Invitation, error) {
	return scanInvitationRow(row)
}

func scanInvitationRow(row rowScanner) (*Invitation, error) {
	var (
		inv                  Invitation
		roleID, usedBy       sql.NullString
		expiresAt, createdAt string
		usedAt, revokedAt    sql.NullString
	)
	if err := row.Scan(&inv.ID, &inv.TokenHash, &inv.CreatedBy, &roleID, &inv.MaxUses, &inv.UseCount,
		&expiresAt, &createdAt, &usedAt, &usedBy, &revokedAt); err != nil {
		return nil, err
	}
	if roleID.Valid {
		inv.RoleID = roleID.String
	}
	if usedBy.Valid {
		inv.UsedBy = usedBy.String
	}
	var err error
	if inv.ExpiresAt, err = db.StringToTime(expiresAt); err != nil {
		return nil, fmt.Errorf("parseando expires_at: %w", err)
	}
	if inv.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if inv.UsedAt, err = db.ParseNullableTime(usedAt.String, usedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando used_at: %w", err)
	}
	if inv.RevokedAt, err = db.ParseNullableTime(revokedAt.String, revokedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando revoked_at: %w", err)
	}
	return &inv, nil
}
