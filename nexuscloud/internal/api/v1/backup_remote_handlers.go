package apiv1

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/porrii/nexuscloud/internal/backup"
)

// requireBackupToken (ADR-029) protege /backups/inbound/* -- nunca una
// sesión de usuario, sino un token compartido dedicado, configurado en
// ambos extremos solo para esto (nunca toca internal/auth/sesiones). Solo
// se monta si h.BackupReceiveToken != "" (ver router.go): si esta
// instancia no está configurada para recibir backups, la capacidad ni
// existe (404 llano, no un 401 que confirmaría que la ruta está ahí).
func (h *Handlers) requireBackupToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-NexusCloud-Backup-Token")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.BackupReceiveToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Token de backup remoto inválido o ausente.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// backupDestination (ADR-029) reutiliza EXACTAMENTE la misma
// implementación (backup.localDestination, vía su constructor exportado)
// que usa un backup hecho en esta máquina -- así un job recibido por red
// queda en disco con el mismo layout, y protegido por la misma
// storage.SafeJoin contra un jobID/relPath que intente escapar de
// BackupsDir, que uno hecho con "backup run" local.
func (h *Handlers) backupDestination() backup.Destination {
	return backup.NewLocalDestination(h.BackupsDir)
}

// UploadInboundBackupFile (ADR-029) recibe un fichero de un backup que
// otro servidor NexusCloud nos está enviando.
func (h *Handlers) UploadInboundBackupFile(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	relPath := chi.URLParam(r, "*")
	if _, err := h.backupDestination().WriteFile(r.Context(), jobID, relPath, r.Body); err != nil {
		h.Logger.Error("recibiendo fichero de backup remoto", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo recibir el fichero.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DownloadInboundBackupFile (ADR-029) sirve de vuelta un fichero ya
// recibido -- usado por Restore/RestoreToPool/Verify del ORIGEN para leer
// un backup que vive aquí.
func (h *Handlers) DownloadInboundBackupFile(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	relPath := chi.URLParam(r, "*")
	f, err := h.backupDestination().OpenFile(r.Context(), jobID, relPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Fichero de backup no encontrado.")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.Copy(w, f); err != nil {
		h.Logger.Warn("transmitiendo fichero de backup remoto", "job_id", jobID, "error", err)
	}
}

// DeleteInboundBackupFile (ADR-029) borra un único fichero -- lo usa el
// ORIGEN cuando el hash de lo que acaba de subir no verifica (ver
// copyToDestination en internal/backup/manager.go), para no dejar un
// fichero corrupto dado por bueno también en el lado remoto.
func (h *Handlers) DeleteInboundBackupFile(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	relPath := chi.URLParam(r, "*")
	if err := h.backupDestination().RemoveFile(r.Context(), jobID, relPath); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo borrar el fichero.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CompleteInboundBackup (ADR-029): el origen llama a esto SOLO tras subir
// con éxito todos los ficheros + manifest.json de un job -- únicamente
// entonces se crea la fila en backup_jobs de ESTE servidor. Si el origen
// nunca llega a llamar esto (se cortó la red a medias), el job recibido
// queda como una carpeta huérfana sin fila -- invisible en "backup list"
// e inofensiva, el mismo invariante que ya sostiene ADR-015 para un
// backup local incompleto, reutilizado aquí en vez de inventar uno nuevo.
func (h *Handlers) CompleteInboundBackup(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	manifest, err := backup.ReadManifest(r.Context(), h.backupDestination(), jobID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "No se pudo leer el manifiesto de este backup.")
		return
	}

	var fileCount, totalBytes int64
	poolIDs := make([]string, 0, len(manifest.Pools))
	for _, pm := range manifest.Pools {
		poolIDs = append(poolIDs, pm.PoolID)
		for _, fm := range pm.Files {
			fileCount++
			totalBytes += fm.SizeBytes
		}
	}
	startedAt := manifest.CreatedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	job := &backup.Job{
		ID: jobID, Status: backup.StatusRunning,
		DestinationPath: h.BackupsDir, PoolIDs: poolIDs, StartedAt: startedAt,
	}
	if err := h.BackupRepo.CreateJob(r.Context(), job); err != nil {
		h.Logger.Error("registrando backup remoto recibido", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo registrar el backup.")
		return
	}
	if err := h.BackupRepo.FinishJob(r.Context(), jobID, backup.StatusCompleted, fileCount, totalBytes, ""); err != nil {
		h.Logger.Error("completando el registro de un backup remoto recibido", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo completar el registro del backup.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteInboundBackupJob (ADR-029) borra TODO lo asociado a un job
// recibido -- ficheros + fila en backup_jobs si llegó a existir --
// usado por la retención del ORIGEN (pruneOldBackups) para podar backups
// antiguos en el destino remoto igual que ya poda uno local.
func (h *Handlers) DeleteInboundBackupJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := h.backupDestination().RemoveJob(r.Context(), jobID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "No se pudo borrar el backup.")
		return
	}
	// ErrJobNotFound es el caso normal de un job que nunca llegó a
	// "complete" (carpeta huérfana sin fila) -- no es un fallo.
	if err := h.BackupRepo.DeleteJob(r.Context(), jobID); err != nil && !errors.Is(err, backup.ErrJobNotFound) {
		h.Logger.Warn("borrando la fila de un backup remoto recibido", "job_id", jobID, "error", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
