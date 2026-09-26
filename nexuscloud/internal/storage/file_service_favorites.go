package storage

import (
	"context"
	"errors"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// ErrFavoritesUnavailable, mismo criterio que ErrUsageUnavailable (quota.go):
// falla en voz alta si se pide una operación de favoritos sin WithFavorites,
// en vez de comportarse como si nunca hubiera ninguno.
var ErrFavoritesUnavailable = errors.New("storage: los favoritos no están disponibles en esta instancia")

// WithFavorites activa favoritos (§87, ADR-038). Sin esta opción,
// AddFavorite/RemoveFavorite/ListFavorites/FavoriteIDsForOwner devuelven
// ErrFavoritesUnavailable.
func WithFavorites(repo FavoriteRepository) FileServiceOption {
	return func(s *FileService) {
		s.favorites = repo
	}
}

// AddFavorite marca como favorito un archivo o carpeta del árbol PROPIO de
// userID (§87: solo lo propio, nunca lo que otros comparten -- decisión de
// producto, ADR-038). Comprueba propiedad igual que el resto de operaciones
// sobre un recurso concreto (ver RevokeShare). Si el recurso ya estaba en
// favoritos, no es un error: devuelve el favorito existente (un botón de
// favorito debe poder pulsarse dos veces sin drama).
func (s *FileService) AddFavorite(ctx context.Context, userID string, isDirectory bool, resourceID string) (*Favorite, error) {
	if s.favorites == nil {
		return nil, ErrFavoritesUnavailable
	}
	if err := s.checkOwnFavoritable(ctx, userID, isDirectory, resourceID); err != nil {
		return nil, err
	}

	f := &Favorite{ID: idgen.New(), UserID: userID, CreatedAt: time.Now().UTC()}
	if isDirectory {
		f.DirectoryID = resourceID
	} else {
		f.FileID = resourceID
	}
	if err := s.favorites.CreateFavorite(ctx, f); err != nil {
		if errors.Is(err, ErrFavoriteAlreadyExists) {
			return s.favorites.GetFavoriteByResource(ctx, userID, isDirectory, resourceID)
		}
		return nil, err
	}
	return f, nil
}

// checkOwnFavoritable comprueba que el recurso existe y pertenece a userID
// -- mismo patrón que ResourceInfoForShare/RevokeShare (file_service_sharing.go),
// pero sin exigir que esté activo: favoritar algo ya en la papelera no es un
// error (es un no-op inofensivo, ListFavorites lo oculta hasta que se
// restaure).
func (s *FileService) checkOwnFavoritable(ctx context.Context, userID string, isDirectory bool, resourceID string) error {
	if isDirectory {
		dir, err := s.directories.GetDirectoryByID(ctx, resourceID)
		if err != nil {
			return err
		}
		if dir.OwnerID != userID {
			return ErrForbidden
		}
		return nil
	}
	meta, err := s.files.GetFileByID(ctx, resourceID)
	if err != nil {
		return err
	}
	if meta.OwnerID != userID {
		return ErrForbidden
	}
	return nil
}

// RemoveFavorite quita un favorito propio (§198 IDOR vía
// FavoriteRepository.RemoveFavorite).
func (s *FileService) RemoveFavorite(ctx context.Context, userID, favoriteID string) error {
	if s.favorites == nil {
		return ErrFavoritesUnavailable
	}
	return s.favorites.RemoveFavorite(ctx, favoriteID, userID)
}

// ListFavorites resuelve los favoritos de userID contra files/directories,
// para la página dedicada de Favoritos. Solo devuelve recursos ACTIVOS: uno
// trasheado se omite (su favorito sigue existiendo y reaparece solo si se
// restaura; ON DELETE CASCADE ya lo limpia si se borra para siempre, sin
// código aquí). Un favorito huérfano (el recurso ya no existe) también se
// omite en silencio -- no debería poder ocurrir salvo una carrera con un
// borrado para siempre justo en este instante, y no es un error del
// usuario que está mirando su lista.
func (s *FileService) ListFavorites(ctx context.Context, userID string) (files []*FileMeta, directories []*Directory, err error) {
	if s.favorites == nil {
		return nil, nil, ErrFavoritesUnavailable
	}
	favs, err := s.favorites.ListFavoritesForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range favs {
		if f.IsDirectoryFavorite() {
			dir, err := s.directories.GetDirectoryByID(ctx, f.DirectoryID)
			if err != nil || dir.DeletedAt != nil {
				continue
			}
			directories = append(directories, dir)
			continue
		}
		meta, err := s.files.GetFileByID(ctx, f.FileID)
		if err != nil || meta.DeletedAt != nil {
			continue
		}
		files = append(files, meta)
	}
	return files, directories, nil
}

// FavoriteIDsForOwner devuelve, para cada recurso favorito de userID, el ID
// del propio favorito (recurso → favorito), para que GET /files pueda
// anotar favorite_id sin que el cliente tenga que consultar dos endpoints.
// Los mapas vienen vacíos (nunca nil) si no hay favoritos, para que el
// llamador no tenga que comprobar nil antes de indexar.
func (s *FileService) FavoriteIDsForOwner(ctx context.Context, userID string) (fileIDs, directoryIDs map[string]string, err error) {
	fileIDs = map[string]string{}
	directoryIDs = map[string]string{}
	if s.favorites == nil {
		return fileIDs, directoryIDs, ErrFavoritesUnavailable
	}
	favs, err := s.favorites.ListFavoritesForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range favs {
		if f.IsDirectoryFavorite() {
			directoryIDs[f.DirectoryID] = f.ID
		} else {
			fileIDs[f.FileID] = f.ID
		}
	}
	return fileIDs, directoryIDs, nil
}
