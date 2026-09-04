package auth

import (
	"context"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/users"
)

// InvitationService crea y canjea invitaciones. Es el único mecanismo de
// alta de usuarios además del CLI de administración: no existe endpoint de
// registro público por defecto (§21).
type InvitationService struct {
	invitations InvitationRepository
	userSvc     *users.Service
	hasher      *Hasher
}

func NewInvitationService(invRepo InvitationRepository, userSvc *users.Service, hasher *Hasher) *InvitationService {
	return &InvitationService{invitations: invRepo, userSvc: userSvc, hasher: hasher}
}

type CreateInvitationInput struct {
	CreatedBy string
	RoleID    string
	MaxUses   int
	TTL       time.Duration
}

func (s *InvitationService) Create(ctx context.Context, in CreateInvitationInput) (invitation *Invitation, plainToken string, err error) {
	if in.MaxUses <= 0 {
		in.MaxUses = 1
	}
	if in.TTL <= 0 {
		in.TTL = 7 * 24 * time.Hour
	}
	token, err := idgen.Token()
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	inv := &Invitation{
		ID:        idgen.New(),
		TokenHash: HashToken(token),
		CreatedBy: in.CreatedBy,
		RoleID:    in.RoleID,
		MaxUses:   in.MaxUses,
		ExpiresAt: now.Add(in.TTL),
		CreatedAt: now,
	}
	if err := s.invitations.CreateInvitation(ctx, inv); err != nil {
		return nil, "", err
	}
	// El token en claro se devuelve solo aquí, una vez (§78).
	return inv, token, nil
}

type RedeemInvitationInput struct {
	Token       string
	Username    string
	DisplayName string
	Email       string
	Password    string
}

// Redeem valida la invitación (expiración/revocación/usos restantes) y crea
// el usuario correspondiente en una única operación lógica.
func (s *InvitationService) Redeem(ctx context.Context, in RedeemInvitationInput) (*users.User, error) {
	inv, err := s.invitations.GetInvitationByTokenHash(ctx, HashToken(in.Token))
	if err != nil {
		return nil, err
	}
	if err := inv.IsUsable(time.Now().UTC()); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}

	u, err := s.userSvc.CreateUser(ctx, users.CreateUserInput{
		Username:     in.Username,
		DisplayName:  in.DisplayName,
		Email:        in.Email,
		PasswordHash: hash,
		Role:         inv.RoleID,
	})
	if err != nil {
		return nil, err
	}

	if err := s.invitations.RecordUse(ctx, inv.ID, u.ID, time.Now().UTC()); err != nil {
		return nil, err
	}
	return u, nil
}
