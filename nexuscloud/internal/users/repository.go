package users

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("users: no encontrado")
	ErrAlreadyExists = errors.New("users: ya existe")
	// ErrLastSuperAdmin: la operación dejaría la instancia sin ningún
	// super_admin activo (ADR-042 Decisión 8).
	ErrLastSuperAdmin = errors.New("users: tiene que quedar al menos un superadministrador activo")
)

// Repository abstrae el acceso a datos de usuarios/roles/grupos para poder
// sustituir el motor de base de datos sin tocar la lógica de negocio (§8).
type Repository interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context) ([]*User, error)
	UpdateUser(ctx context.Context, u *User) error
	DeleteUser(ctx context.Context, id string) error
	CountUsers(ctx context.Context) (int, error)

	AssignRole(ctx context.Context, userID, roleID string) error
	RolesForUser(ctx context.Context, userID string) ([]Role, error)
	HasRole(ctx context.Context, userID, roleID string) (bool, error)
	// SetRole reemplaza todos los roles de userID por roleID en una
	// transacción (ADR-042 Decisión 8). ErrLastSuperAdmin si dejaría la
	// instancia sin ningún super_admin activo.
	SetRole(ctx context.Context, userID, roleID string) error
	// CountActiveSuperAdmins cuenta los super_admin activos excluyendo a
	// excludeUserID (vacío = ninguno).
	CountActiveSuperAdmins(ctx context.Context, excludeUserID string) (int, error)

	CreateGroup(ctx context.Context, g *Group) error
	GetGroupByID(ctx context.Context, id string) (*Group, error)
	GetGroupByName(ctx context.Context, name string) (*Group, error)
	ListGroups(ctx context.Context) ([]*Group, error)
	// UpdateGroupQuota fija la cuota del grupo (nil = sin cuota propia del
	// grupo, 0 = ilimitada). ErrNotFound si el grupo no existe.
	UpdateGroupQuota(ctx context.Context, groupID string, quota *int64) error
	AddUserToGroup(ctx context.Context, userID, groupID string) error
	GroupsForUser(ctx context.Context, userID string) ([]Group, error)
	// ListGroupMembers, RemoveUserFromGroup, RenameGroup y DeleteGroup
	// (ADR-042 Decisión 11): ErrNotFound si el grupo (o la membresía) no
	// existe; RenameGroup da ErrAlreadyExists si el nombre está ocupado.
	ListGroupMembers(ctx context.Context, groupID string) ([]*User, error)
	RemoveUserFromGroup(ctx context.Context, userID, groupID string) error
	RenameGroup(ctx context.Context, groupID, name string) error
	DeleteGroup(ctx context.Context, groupID string) error
}
