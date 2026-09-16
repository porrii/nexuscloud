package apiv1

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/clientupdates"
)

// GetClientUpdatesFeed (ADR-032) sirve releases.{channel}.json reenviado
// desde GitHub Releases -- el cliente de escritorio nunca habla con GitHub
// directamente, solo con su propio servidor NexusCloud. Solo se monta si
// h.ClientUpdatesProxy != nil (ver router.go).
func (h *Handlers) GetClientUpdatesFeed(w http.ResponseWriter, r *http.Request) {
	body, err := h.ClientUpdatesProxy.FeedJSON(r.Context())
	if err != nil {
		h.logClientUpdatesError("obteniendo el feed de actualizaciones", err)
		writeError(w, http.StatusBadGateway, "upstream_error", "No se pudo obtener el feed de actualizaciones desde GitHub.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// DownloadClientUpdateAsset (ADR-032) reenvía un asset (paquete Velopack)
// de la misma release ya resuelta por nombre exacto -- ClientUpdatesProxy
// nunca acepta una URL o ruta arbitraria, así esto no se puede usar para
// pedirle a GitHub cualquier otro fichero del repositorio.
func (h *Handlers) DownloadClientUpdateAsset(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "assetName")
	body, size, err := h.ClientUpdatesProxy.Asset(r.Context(), name)
	if err != nil {
		if errors.Is(err, clientupdates.ErrAssetNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Asset de actualización no encontrado.")
			return
		}
		h.logClientUpdatesError("descargando un asset de actualización", err)
		writeError(w, http.StatusBadGateway, "upstream_error", "No se pudo descargar el asset desde GitHub.")
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if _, err := io.Copy(w, body); err != nil {
		h.logClientUpdatesError("transmitiendo un asset de actualización", err)
	}
}

func (h *Handlers) logClientUpdatesError(msg string, err error) {
	if h.Logger != nil {
		h.Logger.Error(msg, "error", err)
	}
}
