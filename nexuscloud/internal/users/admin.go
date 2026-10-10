package users

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Reglas de administración de cuentas (ADR-042, B1). Viven en el dominio y
// no en los handlers para que la CLI y la API apliquen exactamente las
// mismas (Decisión 4).

var (
	// ErrSelfRoleChange: nadie cambia su propio rol (Decisión 8).
	ErrSelfRoleChange = errors.New("users: no puedes cambiar tu propio rol")
	// ErrSuperAdminProtected: solo un super_admin concede, quita o modifica
	// super_admin, y un administrator no puede editar, desactivar ni borrar
	// a un super_admin (Decisión 8).
	ErrSuperAdminProtected = errors.New("users: solo un superadministrador puede gestionar a otro superadministrador o conceder ese rol")
	// ErrHasPublications: la cuenta tiene publicaciones vigentes que
	// impedirían pasarla a read_only (Decisión 8). Se devuelve envuelto en
	// un *PublicationsError, que lleva el recuento.
	ErrHasPublications = errors.New("users: la cuenta tiene enlaces o comparticiones con subida vigentes")
	// ErrInvalidGroupName: nombre de grupo vacío o demasiado largo.
	ErrInvalidGroupName = errors.New("users: el nombre del grupo es obligatorio (máximo 255 caracteres)")
)

// PublicationCounter cuenta lo que una cuenta tiene publicado y vigente. Lo
// cumple storage.FileService.CountPublicationsForOwner sin que storage
// importe este paquete (mismo patrón que OwnerStatusChecker).
type PublicationCounter interface {
	CountPublicationsForOwner(ctx context.Context, ownerID string) (publicLinks, anonymousUploads, uploadShares int, err error)
}

// WithPublicationCounter conecta el recuento de publicaciones que exige
// pasar una cuenta a read_only. Sin él, ese cambio se rechaza siempre
// (falla cerrado: no se puede comprobar que no haya efectos ocultos).
func WithPublicationCounter(c PublicationCounter) ServiceOption {
	return func(s *Service) { s.publications = c }
}

// PublicationCounts es el recuento que acompaña a ErrHasPublications.
type PublicationCounts struct {
	PublicLinks      int
	AnonymousUploads int
	UploadShares     int
}

func (c PublicationCounts) Total() int { return c.PublicLinks + c.AnonymousUploads + c.UploadShares }

// PublicationsError lleva el recuento para que la API responda 409 con el
// detalle y el administrador sepa qué revocar primero.
type PublicationsError struct{ Counts PublicationCounts }

func (e *PublicationsError) Error() string {
	return fmt.Sprintf("%s (enlaces públicos: %d, subidas anónimas: %d, comparticiones con subida: %d)",
		ErrHasPublications.Error(), e.Counts.PublicLinks, e.Counts.AnonymousUploads, e.Counts.UploadShares)
}

func (e *PublicationsError) Is(target error) bool { return target == ErrHasPublications }

// rolePriority ordena los roles para quedarse con uno cuando una cuenta
// antigua tiene varios (antes de ADR-042 AssignRole los acumulaba).
// read_only va por delante de user: si lo tiene, la restricción manda
// (ADR-044).
var rolePriority = []string{RoleSuperAdmin, RoleAdministrator, RoleReadOnly, RoleUser}

// PrimaryRole devuelve el rol de la cuenta (uno solo, Decisión 8). Una
// cuenta sin ningún rol se trata como RoleUser, igual que al crearla.
func (s *Service) PrimaryRole(ctx context.Context, userID string) (string, error) {
	roles, err := s.repo.RolesForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	return primaryRoleOf(roles), nil
}

func primaryRoleOf(roles []Role) string {
	for _, want := range rolePriority {
		for _, r := range roles {
			if r.ID == want {
				return want
			}
		}
	}
	return RoleUser
}

func isAdminRole(role string) bool { return role == RoleSuperAdmin || role == RoleAdministrator }

// RoleChangeNeedsReauth indica si cambiar de from a to exige reautenticación
// reforzada: todo cambio hacia o desde administrator/super_admin
// (Decisión 2).
func RoleChangeNeedsReauth(from, to string) bool { return isAdminRole(from) || isAdminRole(to) }

// EnsureCanManage comprueba que actorID puede editar, desactivar o borrar a
// targetID: un administrator no toca a un super_admin (Decisión 8). Que el
// actor sea administrador lo garantiza quien llama (RequireAdmin o la CLI,
// que es local).
func (s *Service) EnsureCanManage(ctx context.Context, actorID, targetID string) error {
	targetRole, err := s.PrimaryRole(ctx, targetID)
	if err != nil {
		return err
	}
	if targetRole != RoleSuperAdmin {
		return nil
	}
	actorRole, err := s.PrimaryRole(ctx, actorID)
	if err != nil {
		return err
	}
	if actorRole != RoleSuperAdmin {
		return ErrSuperAdminProtected
	}
	return nil
}

// EnsureCanRemove comprueba, además de EnsureCanManage, que desactivar o
// borrar a targetID no deja la instancia sin super_admin activos.
func (s *Service) EnsureCanRemove(ctx context.Context, actorID, targetID string) error {
	if err := s.EnsureCanManage(ctx, actorID, targetID); err != nil {
		return err
	}
	return s.EnsureNotLastSuperAdmin(ctx, targetID)
}

// EnsureNotLastSuperAdmin devuelve ErrLastSuperAdmin si targetID es el
// único super_admin activo. Sin reglas de actor: la usa también la CLI
// local antes de desactivar o borrar una cuenta.
func (s *Service) EnsureNotLastSuperAdmin(ctx context.Context, targetID string) error {
	role, err := s.PrimaryRole(ctx, targetID)
	if err != nil {
		return err
	}
	if role != RoleSuperAdmin {
		return nil
	}
	others, err := s.repo.CountActiveSuperAdmins(ctx, targetID)
	if err != nil {
		return err
	}
	if others == 0 {
		return ErrLastSuperAdmin
	}
	return nil
}

// SetRole cambia el rol de targetID a role y devuelve el rol anterior
// (Decisión 8). actorID vacío significa la CLI local, que no está sujeta a
// las reglas entre administradores pero sí a la de no dejar la instancia
// sin super_admin (la aplica el repositorio, dentro de la transacción).
func (s *Service) SetRole(ctx context.Context, actorID, targetID, role string) (previous string, err error) {
	if role == "" {
		return "", ErrInvalidRole
	}
	if err := ValidateRole(role); err != nil {
		return "", err
	}
	if actorID != "" && actorID == targetID {
		return "", ErrSelfRoleChange
	}
	if _, err := s.repo.GetUserByID(ctx, targetID); err != nil {
		return "", err
	}
	previous, err = s.PrimaryRole(ctx, targetID)
	if err != nil {
		return "", err
	}
	if actorID != "" && (previous == RoleSuperAdmin || role == RoleSuperAdmin) {
		actorRole, err := s.PrimaryRole(ctx, actorID)
		if err != nil {
			return "", err
		}
		if actorRole != RoleSuperAdmin {
			return "", ErrSuperAdminProtected
		}
	}
	if role == RoleReadOnly && previous != RoleReadOnly {
		if err := s.ensureNothingPublished(ctx, targetID); err != nil {
			return "", err
		}
	}
	if err := s.repo.SetRole(ctx, targetID, role); err != nil {
		return "", err
	}
	return previous, nil
}

func (s *Service) ensureNothingPublished(ctx context.Context, userID string) error {
	if s.publications == nil {
		return errors.New("users: el recuento de publicaciones no está configurado")
	}
	links, anon, uploads, err := s.publications.CountPublicationsForOwner(ctx, userID)
	if err != nil {
		return err
	}
	counts := PublicationCounts{PublicLinks: links, AnonymousUploads: anon, UploadShares: uploads}
	if counts.Total() > 0 {
		return &PublicationsError{Counts: counts}
	}
	return nil
}

// ListGroupMembers, RemoveGroupMember, RenameGroup y DeleteGroup (Decisión
// 11): envoltorios finos sobre el repositorio, aquí para que la CLI y la API
// pasen por el mismo sitio.

func (s *Service) ListGroupMembers(ctx context.Context, groupID string) ([]*User, error) {
	return s.repo.ListGroupMembers(ctx, groupID)
}

func (s *Service) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	return s.repo.RemoveUserFromGroup(ctx, userID, groupID)
}

func (s *Service) RenameGroup(ctx context.Context, groupID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 {
		return ErrInvalidGroupName
	}
	return s.repo.RenameGroup(ctx, groupID, name)
}

func (s *Service) DeleteGroup(ctx context.Context, groupID string) error {
	return s.repo.DeleteGroup(ctx, groupID)
}
