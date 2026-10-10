package apiv1

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/security"
	"github.com/porrii/nexuscloud/internal/storage"
)

// favoriteResponse es la forma que ve el cliente -- resource_type/
// resource_id aplanados, mismo criterio que shareResponse
// (toShareResponse), nunca los dos IDs nullable de storage.Favorite.
type favoriteResponse struct {
	ID           string    `json:"id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	CreatedAt    time.Time `json:"created_at"`
}

func toFavoriteResponse(f *storage.Favorite) favoriteResponse {
	resourceType, resourceID := "file", f.FileID
	if f.IsDirectoryFavorite() {
		resourceType, resourceID = "directory", f.DirectoryID
	}
	return favoriteResponse{ID: f.ID, ResourceType: resourceType, ResourceID: resourceID, CreatedAt: f.CreatedAt}
}

type createFavoriteRequest struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

// favoritesListResponse es la forma de la página dedicada de Favoritos:
// misma forma que listResponse (GET /files), con favorite_id ya poblado en
// cada fila (siempre presente aquí, a diferencia de GET /files donde solo
// aparece si el recurso está favoritado).
type favoritesListResponse struct {
	Directories []directoryResponse `json:"directories"`
	Files       []fileResponse      `json:"files"`
}

// CreateFavorite marca como favorito un archivo o carpeta del árbol PROPIO
// del usuario autenticado (§87, ADR-038: solo lo propio, nunca lo
// compartido). Favoritar dos veces el mismo recurso no es un error: el
// servicio devuelve el favorito ya existente (idempotente).
func (h *Handlers) CreateFavorite(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	var req createFavoriteRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Cuerpo de la petición inválido.")
		return
	}
	if req.ResourceType != "file" && req.ResourceType != "directory" {
		writeError(w, http.StatusBadRequest, "invalid_request", "resource_type debe ser 'file' o 'directory'.")
		return
	}
	if req.ResourceID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "resource_id es obligatorio.")
		return
	}

	fav, err := h.Files.AddFavorite(r.Context(), u.ID, req.ResourceType == "directory", req.ResourceID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventFavoriteAdded, u.ID, req.ResourceType, req.ResourceID, security.ClientIP(r, h.TrustedProxies), nil)
	writeJSON(w, http.StatusCreated, toFavoriteResponse(fav))
}

// ListFavorites devuelve los favoritos del usuario autenticado, resueltos
// contra files/directories (misma forma que GET /files) -- solo recursos
// activos, uno trasheado se oculta hasta que se restaure.
func (h *Handlers) ListFavorites(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	files, directories, err := h.Files.ListFavorites(r.Context(), u.ID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	fileIDs, dirIDs, err := h.Files.FavoriteIDsForOwner(r.Context(), u.ID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	out := favoritesListResponse{
		Directories: make([]directoryResponse, 0, len(directories)),
		Files:       make([]fileResponse, 0, len(files)),
	}
	for _, d := range directories {
		resp := toDirectoryResponse(d)
		if id, ok := dirIDs[d.ID]; ok {
			resp.FavoriteID = &id
		}
		out.Directories = append(out.Directories, resp)
	}
	for _, f := range files {
		resp := toFileResponse(f)
		if id, ok := fileIDs[f.ID]; ok {
			resp.FavoriteID = &id
		}
		out.Files = append(out.Files, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteFavorite exige, vía FavoriteRepository.RemoveFavorite, que el
// favorito pertenezca al usuario autenticado (§198 IDOR).
func (h *Handlers) DeleteFavorite(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.Files.RemoveFavorite(r.Context(), u.ID, id); err != nil {
		writeFileError(w, err)
		return
	}
	h.AuditLog.Record(r.Context(), audit.EventFavoriteRemoved, u.ID, "favorite", id, security.ClientIP(r, h.TrustedProxies), nil)
	w.WriteHeader(http.StatusNoContent)
}
