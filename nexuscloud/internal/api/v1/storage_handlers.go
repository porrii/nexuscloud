package apiv1

import (
	"errors"
	"net/http"

	"github.com/porrii/nexuscloud/internal/storage/diskinfo"
)

// diskResponse es la forma JSON de una unidad de almacenamiento. Tipo
// explícito (§172): nunca se serializa el struct de dominio directamente.
type diskResponse struct {
	MountPoint string `json:"mount_point"`
	Filesystem string `json:"filesystem,omitempty"`
	Device     string `json:"device,omitempty"`
	Label      string `json:"label,omitempty"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	ReadOnly   bool   `json:"read_only"`
}

// ListDisks (admin-only, montado bajo RequireAdmin en router.go) enumera
// las unidades de almacenamiento del host en solo lectura (§11 / requisito
// 3). En un SO sin adaptador devuelve 501 con un cuerpo claro, no un error
// genérico -- así un panel de administración puede distinguir "no
// soportado aquí" de "fallo".
func (h *Handlers) ListDisks(w http.ResponseWriter, r *http.Request) {
	disks, err := diskinfo.Enumerate(r.Context())
	if errors.Is(err, diskinfo.ErrUnsupported) {
		writeError(w, http.StatusNotImplemented, "not_supported",
			"La enumeración de discos no está soportada en el sistema operativo de este servidor.")
		return
	}
	if err != nil {
		h.Logger.Error("enumerando discos", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error",
			"No se pudieron enumerar las unidades de almacenamiento.")
		return
	}

	out := make([]diskResponse, 0, len(disks))
	for _, d := range disks {
		out = append(out, diskResponse{
			MountPoint: d.MountPoint,
			Filesystem: d.Filesystem,
			Device:     d.Device,
			Label:      d.Label,
			TotalBytes: d.TotalBytes,
			FreeBytes:  d.FreeBytes,
			UsedBytes:  d.UsedBytes,
			ReadOnly:   d.ReadOnly,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
