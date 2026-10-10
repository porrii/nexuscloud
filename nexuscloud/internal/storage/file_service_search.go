package storage

import "context"

// Search busca en el árbol PROPIO de requesterID según f (§33). A
// diferencia de List (una carpeta concreta), es recursivo sobre TODO el
// árbol del usuario -- para eso sirve un buscador. search.enabled se
// comprueba en el router (las rutas ni se registran si está desactivado),
// no aquí -- mismo criterio que ClientUpdatesProxy/BackupReceiveToken.
func (s *FileService) Search(ctx context.Context, requesterID string, f SearchFilters) (*ListResult, error) {
	dirs, err := s.directories.SearchDirectories(ctx, requesterID, f)
	if err != nil {
		return nil, err
	}
	files, err := s.files.SearchFiles(ctx, requesterID, f)
	if err != nil {
		return nil, err
	}
	return &ListResult{Directories: dirs, Files: files}, nil
}

// SearchAsAdmin es la variante admin (§33 "Usuario" como criterio): ownerID
// ya viene resuelto por el llamador (el handler resuelve el username de la
// query, mismo patrón que CreateShare con TargetUsername) -- vacío = buscar
// en TODOS los propietarios. Sin comprobación de rol aquí: la exige el
// router (RequireAdmin), igual que el resto de endpoints admin-only.
func (s *FileService) SearchAsAdmin(ctx context.Context, f SearchFilters, ownerID string) (*ListResult, error) {
	dirs, err := s.directories.SearchDirectoriesAllOwners(ctx, f, ownerID)
	if err != nil {
		return nil, err
	}
	files, err := s.files.SearchFilesAllOwners(ctx, f, ownerID)
	if err != nil {
		return nil, err
	}
	return &ListResult{Directories: dirs, Files: files}, nil
}
