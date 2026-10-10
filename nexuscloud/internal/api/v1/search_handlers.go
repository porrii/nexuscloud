package apiv1

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/porrii/nexuscloud/internal/storage"
)

var errInvalidSearchParam = errors.New("parámetro de búsqueda inválido")

// parseSearchFilters lee los query params comunes a /search y /admin/search
// (§33: nombre/ruta vía q, tipo, extensión, fecha, tamaño). date_from/
// date_to esperan RFC3339; size_min/size_max, un entero de bytes. Un valor
// presente pero mal formado es un 400 -- ignorarlo en silencio devolvería
// resultados sin el filtro que el cliente sí pidió, más confuso que un error.
func parseSearchFilters(r *http.Request) (storage.SearchFilters, error) {
	q := r.URL.Query()
	f := storage.SearchFilters{Query: q.Get("q"), MimeType: q.Get("type"), Ext: q.Get("ext")}

	if v := q.Get("date_from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errInvalidSearchParam
		}
		f.DateFrom = &t
	}
	if v := q.Get("date_to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errInvalidSearchParam
		}
		f.DateTo = &t
	}
	if v := q.Get("size_min"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, errInvalidSearchParam
		}
		f.SizeMin = &n
	}
	if v := q.Get("size_max"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, errInvalidSearchParam
		}
		f.SizeMax = &n
	}
	return f, nil
}

// Search busca en el árbol PROPIO del usuario autenticado (§33), en
// cualquier carpeta -- a diferencia de GET /files, es recursivo sobre todo
// el árbol, no una ruta concreta. favorite_id se anota igual que en
// GET /files (mismo criterio de "aditivo, un fallo no rompe el listado").
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	f, err := parseSearchFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "date_from/date_to (RFC3339) o size_min/size_max (bytes) inválidos.")
		return
	}

	result, err := h.Files.Search(r.Context(), u.ID, f)
	if err != nil {
		h.Logger.Error("buscando", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la búsqueda.")
		return
	}

	fileFavIDs, dirFavIDs, err := h.Files.FavoriteIDsForOwner(r.Context(), u.ID)
	if err != nil {
		h.Logger.Warn("no se pudieron resolver los favoritos para anotar la búsqueda", "error", err)
		fileFavIDs, dirFavIDs = map[string]string{}, map[string]string{}
	}
	out := listResponse{
		Directories: make([]directoryResponse, 0, len(result.Directories)),
		Files:       make([]fileResponse, 0, len(result.Files)),
	}
	for _, d := range result.Directories {
		resp := toDirectoryResponse(d)
		if id, ok := dirFavIDs[d.ID]; ok {
			resp.FavoriteID = &id
		}
		out.Directories = append(out.Directories, resp)
	}
	for _, fm := range result.Files {
		resp := toFileResponse(fm)
		if id, ok := fileFavIDs[fm.ID]; ok {
			resp.FavoriteID = &id
		}
		out.Files = append(out.Files, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminSearch busca entre TODOS los usuarios (§33 "Usuario" como criterio),
// o solo uno si se da ?owner=<username> -- resuelto aquí a un ID real, mismo
// patrón que CreateShare con target_username (share_handlers.go). Sin
// favorite_id: son los favoritos del ADMIN, no de cada propietario buscado,
// así que no tiene un significado claro en un listado que cruza usuarios.
func (h *Handlers) AdminSearch(w http.ResponseWriter, r *http.Request) {
	f, err := parseSearchFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "date_from/date_to (RFC3339) o size_min/size_max (bytes) inválidos.")
		return
	}

	var ownerID string
	if username := r.URL.Query().Get("owner"); username != "" {
		target, err := h.UserRepo.GetUserByUsername(r.Context(), username)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "El usuario indicado en 'owner' no existe.")
			return
		}
		ownerID = target.ID
	}

	result, err := h.Files.SearchAsAdmin(r.Context(), f, ownerID)
	if err != nil {
		h.Logger.Error("buscando (admin)", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar la búsqueda.")
		return
	}
	out := listResponse{
		Directories: make([]directoryResponse, 0, len(result.Directories)),
		Files:       make([]fileResponse, 0, len(result.Files)),
	}
	for _, d := range result.Directories {
		out.Directories = append(out.Directories, toDirectoryResponse(d))
	}
	for _, fm := range result.Files {
		out.Files = append(out.Files, toFileResponse(fm))
	}
	writeJSON(w, http.StatusOK, out)
}
