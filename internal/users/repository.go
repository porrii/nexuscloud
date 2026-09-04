package users

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("users: no encontrado")
	ErrAlreadyExists = errors.New("users: ya existe")
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

	CreateGroup(ctx context.Context, g *Group) error
	ListGroups(ctx context.Context) ([]*Group, error)
	AddUserToGroup(ctx context.Context, userID, groupID string) error
	GroupsForUser(ctx context.Context, userID string) ([]Group, error)
}
