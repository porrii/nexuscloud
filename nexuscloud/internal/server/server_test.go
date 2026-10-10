package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/storage"
)

// panickingThumbnailJobRepository implementa storage.ThumbnailJobRepository
// paniqueando en NextPending -- simula el tipo de bug que un decodificador
// de formato complejo produce ante bytes malformados (índice fuera de
// rango, división por cero...), justo el escenario que motivó el hallazgo
// CRÍTICO del pase de security-reviewer sobre el diseño de miniaturas (§34,
// ADR-041 Decisión 6): sin recover(), esto tumbaría el proceso entero.
type panickingThumbnailJobRepository struct {
	invoked chan struct{}
}

func (p panickingThumbnailJobRepository) UpsertPending(ctx context.Context, fileID, sha256Hex string, kind storage.ThumbnailKind) (*storage.ThumbnailJob, error) {
	return nil, nil
}

func (p panickingThumbnailJobRepository) GetByFileID(ctx context.Context, fileID string) (*storage.ThumbnailJob, error) {
	return nil, storage.ErrThumbnailJobNotFound
}

func (p panickingThumbnailJobRepository) NextPending(ctx context.Context) (*storage.ThumbnailJob, error) {
	select {
	case p.invoked <- struct{}{}:
	default:
	}
	panic("fallo simulado de decodificación")
}

func (p panickingThumbnailJobRepository) MarkDone(ctx context.Context, id string) error {
	return nil
}

func (p panickingThumbnailJobRepository) MarkFailedAttempt(ctx context.Context, id, lastError string, maxAttempts int) (bool, error) {
	return false, nil
}

func (p panickingThumbnailJobRepository) ListByStatus(ctx context.Context, status storage.ThumbnailJobStatus, limit, offset int) ([]*storage.ThumbnailJob, error) {
	return nil, nil
}

// TestStartThumbnailLoopSobrevivePanic confirma el tercero y último de los
// tres recover() exigidos por la Decisión 6 (§34, ADR-041): el de
// middleware.Recoverer sobre un handler HTTP lo confirma
// internal/api/v1/router_test.go, el de la goroutine de decodificación con
// timeout lo confirma thumbnail_image_test.go
// (TestGenerateImageThumbnail_RecuperaPanic) -- este es el único de los tres
// sin ningún wrapper HTTP que lo proteja: una goroutine de fondo desde el
// arranque del servidor. Sin su propio recover() dentro de runOnce(), un
// panic aquí tumbaría el proceso entero para todos los inquilinos, y se
// dispara solo (sin que nadie llame a ningún endpoint) en cuanto haya un
// job pendiente.
func TestStartThumbnailLoopSobrevivePanic(t *testing.T) {
	invoked := make(chan struct{}, 1)
	repo := panickingThumbnailJobRepository{invoked: invoked}

	cache, err := storage.NewThumbnailCache(t.TempDir(), 1<<30)
	if err != nil {
		t.Fatalf("NewThumbnailCache: %v", err)
	}

	// Las demás dependencias de FileService quedan a nil a propósito:
	// NewPending panica en la primerísima línea de ProcessNextThumbnailJob,
	// antes de que se toque ningún otro campo -- no aporta nada real
	// construir un FileService completo (BD, pools, providers) solo para
	// probar que UN recover() concreto sigue en su sitio.
	fileSvc := storage.NewFileService(nil, nil, nil, nil, nil, nil, nil,
		false, false, 0, 0, 0, false, false,
		storage.WithThumbnails(repo, cache, t.TempDir(), storage.ThumbnailLimits{MaxConcurrentGenerations: 1}, true))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Repository nil: Record() nunca llega a invocarse porque el panic
	// ocurre antes de que runOnce() examine result.BecameFailed.
	auditLog := audit.NewRecorder(nil, logger)

	stop := make(chan struct{})
	defer close(stop)
	startThumbnailLoop(fileSvc, auditLog, logger, stop)

	select {
	case <-invoked:
		// NextPending ya paniqueó y runOnce() ya lo recuperó -- de lo
		// contrario el panic habría propagado por la goroutine y abortado
		// TODO el binario de test, no solo esta goroutine, así que nunca
		// habríamos llegado a leer de este canal.
	case <-time.After(5 * time.Second):
		t.Fatal("el bucle nunca invocó NextPending")
	}
}
