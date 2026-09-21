package apiv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/porrii/nexuscloud/internal/storage"
	"github.com/porrii/nexuscloud/internal/users"
)

// Mensajes de la respuesta 507 (§24, ADR-036). El del propio usuario le dice
// qué hacer; el de las carpetas ajenas (compartidas o por enlace público) es
// genérico a propósito: quien sube no debe averiguar cuánto espacio usa ni
// cuánta cuota tiene el propietario.
const (
	quotaExceededMessage   = "No hay espacio suficiente: has alcanzado tu cuota de almacenamiento. Libera espacio (la papelera y las versiones anteriores también cuentan) o pide al administrador que la amplíe."
	noSpaceInFolderMessage = "No hay espacio disponible en esta carpeta."
)

// writeQuotaExceeded responde 507 Insufficient Storage (RFC 4918), el estado
// que entienden los clientes WebDAV. El 413 se queda para el límite de tamaño
// por archivo (upload_too_large).
func writeQuotaExceeded(w http.ResponseWriter) {
	writeError(w, http.StatusInsufficientStorage, "quota_exceeded", quotaExceededMessage)
}

func writeNoSpaceInFolder(w http.ResponseWriter) {
	writeError(w, http.StatusInsufficientStorage, "quota_exceeded", noSpaceInFolderMessage)
}

// quotaResponse es GET /users/me/quota: el uso de quien pregunta, desglosado, y
// su límite efectivo. limit_bytes se omite si no tiene límite; source dice de
// dónde sale (user | group | global | none).
type quotaResponse struct {
	UsedBytes     int64  `json:"used_bytes"`
	FilesBytes    int64  `json:"files_bytes"`
	TrashBytes    int64  `json:"trash_bytes"`
	VersionsBytes int64  `json:"versions_bytes"`
	LimitBytes    *int64 `json:"limit_bytes,omitempty"`
	Source        string `json:"source"`
	GroupName     string `json:"group_name,omitempty"`
}

func toQuotaResponse(q users.EffectiveQuota, usage storage.Usage) quotaResponse {
	out := quotaResponse{
		UsedBytes:     usage.Total(),
		FilesBytes:    usage.FilesBytes,
		TrashBytes:    usage.TrashBytes,
		VersionsBytes: usage.VersionsBytes,
		Source:        string(q.Source),
		GroupName:     q.GroupName,
	}
	if !q.Unlimited() {
		limit := q.LimitBytes
		out.LimitBytes = &limit
	}
	return out
}

// MyQuota informa del uso y el límite de quien la pide. Es un endpoint aparte
// de /users/me a propósito: /users/me se llama en cada carga de página y esto
// hace sumas sobre los archivos.
func (h *Handlers) MyQuota(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	effective, err := h.UserSvc.EffectiveQuota(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("resolviendo la cuota", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo obtener la cuota.")
		return
	}
	usage, err := h.Files.Usage(r.Context(), u.ID)
	if err != nil {
		h.Logger.Error("midiendo el uso", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo obtener el uso.")
		return
	}
	writeJSON(w, http.StatusOK, toQuotaResponse(effective, usage))
}

// quotaField es un campo quota_bytes de un cuerpo JSON ya interpretado. Es
// tri-estado: ausente (no se toca), null (sin cuota propia: hereda) o un
// número (0 = ilimitada, >0 = límite en bytes). Con un *int64 normal no se
// distinguiría «ausente» de «null», de ahí el json.RawMessage de origen.
type quotaField struct {
	present bool   // el campo venía en el cuerpo
	value   *int64 // nil = hereda / sin cuota propia
}

var errInvalidQuotaField = errors.New("quota_bytes debe ser un entero mayor o igual que 0 (0 = ilimitada) o null (hereda)")

func parseQuotaField(raw json.RawMessage) (quotaField, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return quotaField{}, nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return quotaField{present: true}, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return quotaField{}, errInvalidQuotaField
	}
	if err := users.ValidateQuota(&n); err != nil {
		return quotaField{}, errInvalidQuotaField
	}
	return quotaField{present: true, value: &n}, nil
}

// quotaValue convierte una cuota opcional al valor de los metadatos de
// auditoría: null si no hay cuota propia.
func quotaValue(q *int64) any {
	if q == nil {
		return nil
	}
	return *q
}

func sameQuota(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
