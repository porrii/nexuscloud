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

type versionResponse struct {
	VersionNum int       `json:"version_num"`
	SizeBytes  int64     `json:"size_bytes"`
	SHA256     string    `json:"sha256"`
	MimeType   string    `json:"mime_type"`
	CreatedAt  time.Time `json:"created_at"`
}

func toVersionResponse(v *storage.FileVersion) versionResponse {
	return versionResponse{
		VersionNum: v.VersionNum, SizeBytes: v.SizeBytes, SHA256: v.SHA256, MimeType: v.MimeType, CreatedAt: v.CreatedAt,
	}
}

type groupResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toGroupResponse(g *users.Group) groupResponse {
	return groupResponse{ID: g.ID, Name: g.Name}
}

// shareResponse nunca expone PasswordHash/TokenHash (§172): HasPassword es
// un booleano derivado, y Token solo se rellena en la respuesta de creación
// de un enlace -- una única vez, igual que sesiones/invitaciones (§78) -- el
// resto de respuestas (listar, etc.) lo dejan vacío.
type shareResponse struct {
	ID                 string     `json:"id"`
	ResourceType       string     `json:"resource_type"`
	ResourceID         string     `json:"resource_id"`
	ResourceName       string     `json:"resource_name,omitempty"`
	ShareType          string     `json:"share_type"`
	TargetUserID       string     `json:"target_user_id,omitempty"`
	TargetUsername     string     `json:"target_username,omitempty"`
	TargetGroupID      string     `json:"target_group_id,omitempty"`
	TargetGroupName    string     `json:"target_group_name,omitempty"`
	Label              string     `json:"label,omitempty"`
	CanDownload        bool       `json:"can_download"`
	CanUpload          bool       `json:"can_upload"`
	HasPassword        bool       `json:"has_password"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	MaxDownloads       *int       `json:"max_downloads,omitempty"`
	DownloadCount      int        `json:"download_count"`
	MaxUploadSizeBytes *int64     `json:"max_upload_size_bytes,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	Token              string     `json:"token,omitempty"`
}

// shareResponseExtra agrupa los nombres resueltos (usuario/grupo destino,
// recurso) que toShareResponse no puede resolver por sí sola -- son I/O
// adicional que corresponde al handler, no a un mapper DTO puro.
type shareResponseExtra struct {
	ResourceName    string
	TargetUsername  string
	TargetGroupName string
}

func toShareResponse(s *storage.Share, extra shareResponseExtra) shareResponse {
	resourceType, resourceID := "file", s.FileID
	if s.IsDirectoryShare() {
		resourceType, resourceID = "directory", s.DirectoryID
	}
	return shareResponse{
		ID:                 s.ID,
		ResourceType:       resourceType,
		ResourceID:         resourceID,
		ResourceName:       extra.ResourceName,
		ShareType:          string(s.Type),
		TargetUserID:       s.TargetUserID,
		TargetUsername:     extra.TargetUsername,
		TargetGroupID:      s.TargetGroupID,
		TargetGroupName:    extra.TargetGroupName,
		Label:              s.Label,
		CanDownload:        s.CanDownload,
		CanUpload:          s.CanUpload,
		HasPassword:        s.HasPassword(),
		ExpiresAt:          s.ExpiresAt,
		MaxDownloads:       s.MaxDownloads,
		DownloadCount:      s.DownloadCount,
		MaxUploadSizeBytes: s.MaxUploadSizeBytes,
		CreatedAt:          s.CreatedAt,
	}
}
