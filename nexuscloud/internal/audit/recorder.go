package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// Recorder añade generación de ID/timestamp sobre Repository. Un fallo al
// registrar auditoría se loguea pero nunca aborta la operación de negocio
// que lo originó (una subida de archivo no debe fallar porque el audit log
// tuvo un problema transitorio) — pero tampoco se oculta (§94).
type Recorder struct {
	repo   Repository
	logger *slog.Logger
}

func NewRecorder(repo Repository, logger *slog.Logger) *Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Recorder{repo: repo, logger: logger}
}

func (r *Recorder) Record(ctx context.Context, eventType, actorUserID, targetType, targetID, ip string, metadata map[string]any) {
	e := &Event{
		ID:          idgen.New(),
		OccurredAt:  time.Now().UTC(),
		ActorUserID: actorUserID,
		EventType:   eventType,
		TargetType:  targetType,
		TargetID:    targetID,
		IP:          ip,
		Metadata:    metadata,
	}
	if err := r.repo.RecordEvent(ctx, e); err != nil {
		r.logger.Error("no se pudo registrar evento de auditoría", "event_type", eventType, "error", err)
	}
}
