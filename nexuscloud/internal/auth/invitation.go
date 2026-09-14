package auth

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvitationNotFound  = errors.New("auth: invitación no encontrada o inválida")
	ErrInvitationExpired   = errors.New("auth: invitación expirada")
	ErrInvitationExhausted = errors.New("auth: invitación sin usos restantes")
	ErrInvitationRevoked   = errors.New("auth: invitación revocada")
)

// Invitation es el único mecanismo (junto con el CLI de admin) para crear
// cuentas: no hay registro público por defecto (§21).
type Invitation struct {
	ID        string
	TokenHash string
	CreatedBy string
	RoleID    string // vacío → se asigna users.RoleUser al canjear
	MaxUses   int
	UseCount  int
	ExpiresAt time.Time
	CreatedAt time.Time
	UsedAt    *time.Time
	UsedBy    string
	RevokedAt *time.Time
}

// IsUsable comprueba expiración, revocación y usos restantes antes de
// canjear una invitación.
func (i *Invitation) IsUsable(now time.Time) error {
	if i.RevokedAt != nil {
		return ErrInvitationRevoked
	}
	if now.After(i.ExpiresAt) {
		return ErrInvitationExpired
	}
	if i.UseCount >= i.MaxUses {
		return ErrInvitationExhausted
	}
	return nil
}

type InvitationRepository interface {
	CreateInvitation(ctx context.Context, inv *Invitation) error
	GetInvitationByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	ListInvitations(ctx context.Context) ([]*Invitation, error)
	RecordUse(ctx context.Context, id, usedByUserID string, usedAt time.Time) error
	RevokeInvitation(ctx context.Context, id string) error
}
