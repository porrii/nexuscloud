// Package backup implementa el Backup Manager (§18, ADR-015): backup manual
// completo de Storage Pools a una carpeta de destino, con verificación de
// integridad y restauración. RAID/snapshots (§12/§17) y backup incremental/
// programado/cifrado (§18/§173) quedan para slices futuros.
package backup

import (
	"context"
	"errors"
	"time"
)

var (
	ErrJobNotFound       = errors.New("backup: backup job no encontrado")
	ErrNoEligiblePools   = errors.New("backup: no hay ningún storage pool elegible para respaldar")
	ErrJobNotRestorable  = errors.New("backup: este backup no se completó correctamente y no se puede restaurar")
	ErrIntegrityMismatch = errors.New("backup: el contenido copiado no coincide con el hash esperado")
	ErrPoolNotActive     = errors.New("backup: el pool destino no está activo")
	// ErrPassphraseRequired (ADR-028): --encrypt/backup.encrypt está activo,
	// o el job a restaurar/verificar está cifrado, sin
	// NEXUSCLOUD_BACKUP_PASSPHRASE definida en el entorno.
	ErrPassphraseRequired = errors.New("backup: este backup está cifrado; define NEXUSCLOUD_BACKUP_PASSPHRASE")
)

const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Job es una invocación de "backup run", que puede cubrir varios pools. El
// detalle por fichero NO vive aquí (multiplicaría filas de BD en un backup
// grande) sino en un manifest.json autocontenido dentro de
// DestinationPath/ID/ -- ver manifest.go.
type Job struct {
	ID              string
	Status          string // running | completed | failed
	DestinationPath string
	PoolIDs         []string
	FileCount       int64
	TotalBytes      int64
	StartedAt       time.Time
	FinishedAt      *time.Time
	ErrorMessage    string
}

type Repository interface {
	CreateJob(ctx context.Context, j *Job) error
	// FinishJob marca un job existente como completed o failed, con sus
	// contadores finales. errorMessage vacío para un job completado.
	FinishJob(ctx context.Context, id, status string, fileCount, totalBytes int64, errorMessage string) error
	GetJobByID(ctx context.Context, id string) (*Job, error)
	// ListJobs devuelve todos los jobs, más reciente primero.
	ListJobs(ctx context.Context) ([]*Job, error)
	// DeleteJob borra la fila de un job (usado por la retención, ADR-017).
	// No toca nada en disco -- eso es responsabilidad del llamador, igual
	// que DeleteFile en storage.FileRepository no toca el Provider.
	DeleteJob(ctx context.Context, id string) error
}
