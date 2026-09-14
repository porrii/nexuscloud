// Package users modela usuarios, roles y grupos (§20-24). No sabe nada de
// contraseñas en claro, hashing ni sesiones — eso es responsabilidad de
// internal/auth, que depende de este paquete (no al revés) para evitar
// ciclos de importación.
package users

import "time"

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// Roles predefinidos (§22), sembrados por la migración inicial.
const (
	RoleSuperAdmin    = "super_admin"
	RoleAdministrator = "administrator"
	RoleUser          = "user"
	RoleReadOnly      = "read_only"
)

type User struct {
	ID           string
	Username     string
	DisplayName  string
	Email        string // vacío = sin email (§20: opcional)
	PasswordHash string
	Status       Status
	QuotaBytes   *int64 // nil = usa cuota de grupo/global (§24)
	TOTPSecret   string // vacío = 2FA no habilitado (§25)
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

func (u *User) IsActive() bool { return u.Status == StatusActive }
func (u *User) HasTOTP() bool  { return u.TOTPSecret != "" }

type Role struct {
	ID   string
	Name string
}

type Group struct {
	ID         string
	Name       string
	QuotaBytes *int64
	CreatedAt  time.Time
}
