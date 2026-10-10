package storage

import (
	"context"
	"errors"
	"time"
)

// ErrThumbnailJobNotFound: no existe ningún job (pendiente o fallido) para
// ese file_id -- el llamador (GenerateOrGetThumbnail) lo usa para decidir
// si crear uno nuevo con UpsertPending o reutilizar el que ya había, sin
// resetear attempts/status de un job en curso solo por consultarlo.
var ErrThumbnailJobNotFound = errors.New("storage: no hay ningún job de miniatura para este archivo")

// ThumbnailKind decide qué pipeline procesa un job (§34, ADR-041): image
// se decodifica en proceso (stdlib de Go + x/image/webp); video/pdf
// invocan un subproceso externo (ffmpeg/pdftoppm).
type ThumbnailKind string

const (
	ThumbnailKindImage ThumbnailKind = "image"
	ThumbnailKindVideo ThumbnailKind = "video"
	ThumbnailKindPDF   ThumbnailKind = "pdf"
)

// ThumbnailKindForMimeType clasifica un mime_type ya calculado
// (detectMimeType) en el pipeline que le corresponde, o "" si no hay
// miniatura posible para ese tipo -- el llamador (Upload) usa "" para
// decidir que no hace falta encolar ningún job.
func ThumbnailKindForMimeType(mimeType string) ThumbnailKind {
	switch {
	case mimeType == "application/pdf":
		return ThumbnailKindPDF
	case len(mimeType) >= 6 && mimeType[:6] == "video/":
		return ThumbnailKindVideo
	case len(mimeType) >= 6 && mimeType[:6] == "image/":
		return ThumbnailKindImage
	default:
		return ""
	}
}

type ThumbnailJobStatus string

const (
	ThumbnailJobPending ThumbnailJobStatus = "pending"
	ThumbnailJobFailed  ThumbnailJobStatus = "failed"
)

// ThumbnailJob es un trabajo pendiente o fallido de generación de
// miniatura (§34, ADR-041) -- un job resuelto con éxito se BORRA
// (ThumbnailJobRepository.MarkDone), no se conserva: lo único que le
// importa a un administrador es lo pendiente o roto, no un historial de
// éxitos.
type ThumbnailJob struct {
	ID        string
	FileID    string
	SHA256    string
	Kind      ThumbnailKind
	Status    ThumbnailJobStatus
	Attempts  int
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ThumbnailJobRepository abstrae el acceso a datos de la cola de
// miniaturas (§8).
type ThumbnailJobRepository interface {
	// UpsertPending crea el job de un archivo si no existía, o lo reinicia
	// a pending/attempts=0 si ya existía (mismo criterio que UpsertFile:
	// resubir/reprocesar un archivo no debe dejar un job en failed
	// colgado para siempre). Devuelve el job tal como quedó persistido.
	UpsertPending(ctx context.Context, fileID, sha256 string, kind ThumbnailKind) (*ThumbnailJob, error)
	// GetByFileID devuelve el job de un archivo tal cual está (sin tocar
	// attempts/status), o ErrThumbnailJobNotFound si no existe ninguno --
	// a diferencia de UpsertPending, esta consulta NUNCA reinicia un job
	// en curso solo por mirarlo (GenerateOrGetThumbnail la usa para no
	// perder la cuenta de intentos de un job que el bucle en segundo
	// plano ya está trabajando).
	GetByFileID(ctx context.Context, fileID string) (*ThumbnailJob, error)
	// NextPending toma el job pendiente más antiguo, o nil si no hay
	// ninguno -- consumido secuencialmente por el bucle en segundo plano,
	// nunca en paralelo (sin worker pool, §34 Decisión 1).
	NextPending(ctx context.Context) (*ThumbnailJob, error)
	// MarkDone borra el job -- un éxito no necesita seguir ocupando
	// espacio en la tabla.
	MarkDone(ctx context.Context, id string) error
	// MarkFailedAttempt incrementa attempts y guarda lastError; si
	// attempts alcanza maxAttempts pasa a status=failed (deja de
	// reintentarse solo) y devuelve becameFailed=true, para que el
	// llamador decida si audita el evento (solo la primera vez que entra
	// en failed, no en cada intento intermedio).
	MarkFailedAttempt(ctx context.Context, id, lastError string, maxAttempts int) (becameFailed bool, err error)
	// ListByStatus pagina los jobs de un estado (GET /admin/thumbnail-jobs).
	ListByStatus(ctx context.Context, status ThumbnailJobStatus, limit, offset int) ([]*ThumbnailJob, error)
}
