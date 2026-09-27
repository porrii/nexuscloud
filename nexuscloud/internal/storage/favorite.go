package storage

import (
	"context"
	"errors"
	"time"
)

var ErrFavoriteNotFound = errors.New("storage: favorito no encontrado")

// Favorite marca un archivo o una carpeta del árbol PROPIO de un usuario
// (§87, ADR-038) -- exactamente una de FileID/DirectoryID, nunca ambas ni
// ninguna, mismo patrón que Share (share.go). Los campos "" son "sin
// valor", igual criterio que el resto del paquete (nunca punteros a
// string).
type Favorite struct {
	ID          string
	UserID      string
	FileID      string // "" si el recurso favorito es una carpeta
	DirectoryID string // "" si el recurso favorito es un archivo
	CreatedAt   time.Time
}

func (f *Favorite) IsDirectoryFavorite() bool { return f.DirectoryID != "" }

// FavoriteRepository abstrae el acceso a datos de favoritos (§8).
type FavoriteRepository interface {
	// CreateFavorite inserta un favorito nuevo. Si ya existe uno para el
	// mismo (UserID, recurso) devuelve ErrFavoriteAlreadyExists -- el
	// llamador (FileService.AddFavorite) decide si eso es un error real o
	// si debe resolverse a "ya estaba, aquí está" (idempotencia deseada
	// para un botón de favorito que se pueda pulsar dos veces sin drama).
	CreateFavorite(ctx context.Context, f *Favorite) error
	GetFavoriteByResource(ctx context.Context, userID string, isDirectory bool, resourceID string) (*Favorite, error)
	// RemoveFavorite exige coincidencia de userID en la propia cláusula
	// WHERE: defensa en profundidad contra IDOR (§198), mismo criterio que
	// SessionRepository.RevokeSession y los tokens WebDAV/API.
	RemoveFavorite(ctx context.Context, id, userID string) error
	// ListFavoritesForUser devuelve los IDs de recurso favoritos de un
	// usuario, agrupados por tipo, junto con el ID del propio favorito (para
	// poder quitarlo después) -- lo que necesita tanto la página de
	// Favoritos (resolver contra files/directories) como anotar
	// GET /files con favorite_id.
	ListFavoritesForUser(ctx context.Context, userID string) ([]*Favorite, error)
}

// ErrFavoriteAlreadyExists señala una violación del UNIQUE(user_id,
// file_id)/UNIQUE(user_id, directory_id) -- nunca llega hasta la API: la
// capa de servicio la traduce a "aquí tienes el que ya existía".
var ErrFavoriteAlreadyExists = errors.New("storage: ese recurso ya está en favoritos")
