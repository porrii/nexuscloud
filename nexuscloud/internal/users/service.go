package users

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

var ErrInvalidUsername = errors.New("users: nombre de usuario inválido (minúsculas/números/./_/-, 2-32 caracteres, empieza por alfanumérico)")

// Service contiene la lógica de negocio sobre usuarios/roles/grupos. No
// conoce hashing de contraseñas: CreateUserInput.PasswordHash debe llegar
// ya hasheado desde internal/auth (§25), que es quien depende de este
// paquete y no al revés.
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

type CreateUserInput struct {
	Username     string
	DisplayName  string
	Email        string
	PasswordHash string
	Role         string // vacío → RoleUser
	QuotaBytes   *int64
}

// CreateUser crea un usuario y le asigna su rol (por defecto RoleUser, §22).
// No hay registro público (§21): esto solo debe invocarse desde el CLI de
// administración o desde el canje de una invitación.
func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	if !usernamePattern.MatchString(in.Username) {
		return nil, ErrInvalidUsername
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	role := in.Role
	if role == "" {
		role = RoleUser
	}

	now := time.Now().UTC()
	u := &User{
		ID:           idgen.New(),
		Username:     in.Username,
		DisplayName:  in.DisplayName,
		Email:        in.Email,
		PasswordHash: in.PasswordHash,
		Status:       StatusActive,
		QuotaBytes:   in.QuotaBytes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	if err := s.repo.AssignRole(ctx, u.ID, role); err != nil {
		return nil, fmt.Errorf("asignando rol por defecto: %w", err)
	}
	return u, nil
}

// Disable deshabilita un usuario (§20: Status). No lo elimina: preserva sus
// archivos y metadatos.
func (s *Service) Disable(ctx context.Context, userID string) error {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	u.Status = StatusDisabled
	u.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateUser(ctx, u)
}

// IsFirstUser indica si todavía no existe ningún usuario, para decidir si
// el siguiente `admin create-user` debe recibir el rol super_admin (§140).
func (s *Service) IsFirstUser(ctx context.Context) (bool, error) {
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// IsAdmin comprueba si un usuario tiene rol super_admin o administrator.
func (s *Service) IsAdmin(ctx context.Context, userID string) (bool, error) {
	roles, err := s.repo.RolesForUser(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, r := range roles {
		if r.ID == RoleSuperAdmin || r.ID == RoleAdministrator {
			return true, nil
		}
	}
	return false, nil
}
