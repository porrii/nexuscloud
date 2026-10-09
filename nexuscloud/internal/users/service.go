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
	// defaultQuotaBytes es la cuota global (0 = sin ella); ver WithDefaultQuota.
	defaultQuotaBytes int64
}

func NewService(repo Repository, opts ...ServiceOption) *Service {
	s := &Service{repo: repo}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
	if err := ValidateQuota(in.QuotaBytes); err != nil {
		return nil, err
	}
	if err := ValidateRole(in.Role); err != nil {
		return nil, err
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
		QuotaBytes:   copyQuota(in.QuotaBytes),
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

// IsActive indica si un usuario existe y está activo (ADR-043). Un usuario
// inexistente da (false, nil): para quien pregunta (p. ej. la resolución de
// un enlace público) es lo mismo que uno desactivado. Cumple
// storage.OwnerStatusChecker sin que storage importe este paquete.
func (s *Service) IsActive(ctx context.Context, userID string) (bool, error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return u.IsActive(), nil
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

// IsReadOnly indica si el usuario tiene el rol read_only (ADR-044). Basta
// con tenerlo: si lo combina con otro rol, gana la restricción.
func (s *Service) IsReadOnly(ctx context.Context, userID string) (bool, error) {
	return s.repo.HasRole(ctx, userID, RoleReadOnly)
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
