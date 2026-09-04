package apiv1

import (
	"time"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// Las respuestas JSON son tipos explícitos, nunca los structs de dominio
// directamente: así PasswordHash/TOTPSecret/TokenHash nunca pueden filtrarse
// por un descuido futuro al añadir un campo al dominio (§172).

type userResponse struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"`
	HasTOTP     bool       `json:"has_totp"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func toUserResponse(u *users.User) userResponse {
	return userResponse{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Status:      string(u.Status),
		HasTOTP:     u.HasTOTP(),
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

type sessionResponse struct {
	ID         string    `json:"id"`
	Device     string    `json:"device,omitempty"`
	IP         string    `json:"ip,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func toSessionResponse(s *auth.Session) sessionResponse {
	return sessionResponse{
		ID: s.ID, Device: s.Device, IP: s.IP,
		CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt,
	}
}

type directoryResponse struct {
	ID         string     `json:"id"`
	ParentPath string     `json:"parent_path"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

func toDirectoryResponse(d *storage.Directory) directoryResponse {
	return directoryResponse{ID: d.ID, ParentPath: d.ParentPath, Name: d.Name, CreatedAt: d.CreatedAt, DeletedAt: d.DeletedAt}
}

type fileResponse struct {
	ID         string     `json:"id"`
	ParentPath string     `json:"parent_path"`
	Name       string     `json:"name"`
	SizeBytes  int64      `json:"size_bytes"`
	SHA256     string     `json:"sha256"`
	MimeType   string     `json:"mime_type"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
}

func toFileResponse(f *storage.FileMeta) fileResponse {
	return fileResponse{
		ID: f.ID, ParentPath: f.ParentPath, Name: f.Name, SizeBytes: f.SizeBytes,
		SHA256: f.SHA256, MimeType: f.MimeType, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, DeletedAt: f.DeletedAt,
	}
}
