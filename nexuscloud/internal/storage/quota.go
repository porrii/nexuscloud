package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrQuotaExceeded se devuelve cuando una subida no cabe en la cuota del
	// propietario de los datos (§24, ADR-036).
	ErrQuotaExceeded = errors.New("storage: no hay espacio suficiente: se ha alcanzado la cuota de almacenamiento")
	// ErrUsageUnavailable se devuelve al pedir el uso (o al comprobar una
	// cuota) en un FileService construido sin WithQuotas.
	ErrUsageUnavailable = errors.New("storage: la medición de uso no está configurada")
)

// QuotaResolver da el límite efectivo de un propietario en bytes (0 = sin
// límite). Lo cumple users.Service.LimitFor: storage declara aquí lo que
// necesita para no importar el paquete de usuarios.
type QuotaResolver interface {
	LimitFor(ctx context.Context, ownerID string) (int64, error)
}

// FileServiceOption configura opcionalmente un FileService (las llamadas a
// NewFileService sin opciones se comportan exactamente como antes).
type FileServiceOption func(*FileService)

// WithQuotas activa las cuotas (ADR-036): resolver da el límite de cada
// propietario y usage mide su huella. Sin esta opción no hay ninguna
// comprobación de cuota y Usage devuelve ErrUsageUnavailable.
func WithQuotas(resolver QuotaResolver, usage UsageRepository) FileServiceOption {
	return func(s *FileService) {
		s.quotaResolver = resolver
		s.usage = usage
	}
}

// Usage devuelve la huella en disco de un propietario (archivos + papelera +
// versiones).
func (s *FileService) Usage(ctx context.Context, ownerID string) (Usage, error) {
	if s.usage == nil {
		return Usage{}, ErrUsageUnavailable
	}
	return s.usage.OwnerUsage(ctx, ownerID)
}

// quotaGate es la decisión de cuota de UNA subida, tomada antes de leer el
// cuerpo: el límite del propietario y cuántos bytes caben en ese momento.
type quotaGate struct {
	limit int64 // > 0
	avail int64 // >= 0: lo que cabe al empezar
}

// beginQuota devuelve nil (sin comprobación) si no hay cuotas, si la subida
// está exenta o si el propietario no tiene límite. Si no se puede averiguar
// el límite falla, en vez de dejar subir «sin límite» en silencio.
//
// avail cuenta como crédito el tamaño del archivo activo que esta subida
// reemplaza, sin saber aún si el contenido anterior se liberará (sin
// versionado, o con el mismo hash) o se conservará como versión: es una cota
// superior optimista que solo sirve para cortar pronto lo que no cabe de
// ningún modo. La decisión definitiva es checkQuotaAtCommit.
func (s *FileService) beginQuota(ctx context.Context, in UploadInput, poolID, parent string) (*quotaGate, error) {
	if s.quotaResolver == nil || in.SkipQuota {
		return nil, nil
	}
	limit, err := s.quotaResolver.LimitFor(ctx, in.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo la cuota del propietario: %w", err)
	}
	if limit <= 0 {
		return nil, nil
	}
	if s.usage == nil {
		return nil, ErrUsageUnavailable
	}
	used, err := s.usage.OwnerUsage(ctx, in.OwnerID)
	if err != nil {
		return nil, err
	}
	credit, err := s.replaceableBytes(ctx, poolID, in.OwnerID, parent, in.Name)
	if err != nil {
		return nil, err
	}

	avail := max(0, limit-used.Total()+credit)
	if in.SizeHint > 0 && in.SizeHint > avail {
		return nil, ErrQuotaExceeded
	}
	return &quotaGate{limit: limit, avail: avail}, nil
}

// replaceableBytes es el tamaño del archivo ACTIVO que una subida con esa
// clave natural reemplazaría (0 si no hay ninguno o está en la papelera).
func (s *FileService) replaceableBytes(ctx context.Context, poolID, ownerID, parent, name string) (int64, error) {
	existing, err := s.files.GetFileByNaturalKey(ctx, poolID, ownerID, parent, name)
	switch {
	case err == nil && !existing.IsTrashed():
		return existing.SizeBytes, nil
	case err != nil && !errors.Is(err, ErrFileNotFound):
		return 0, err
	}
	return 0, nil
}

// checkQuotaAtCommit es la comprobación definitiva de una subida: se hace con
// el contenido ya escrito en staging (se sabe el tamaño y el hash reales) y
// con el cerrojo del propietario tomado, así que dos subidas simultáneas no
// pueden pasarse entre las dos. Si la subida reemplaza un archivo activo, su
// contenido anterior solo se libera cuando no se guarda como versión: sin
// versionado, o cuando el contenido nuevo es idéntico (no hay versión nueva).
func (s *FileService) checkQuotaAtCommit(ctx context.Context, gate *quotaGate, ownerID string, size int64, sha string, existing *FileMeta, hasActiveExisting bool) error {
	used, err := s.usage.OwnerUsage(ctx, ownerID)
	if err != nil {
		return err
	}
	var credit int64
	if hasActiveExisting && (!s.versioningEnabled || existing.SHA256 == sha) {
		credit = existing.SizeBytes
	}
	if size > max(0, gate.limit-used.Total()+credit) {
		return ErrQuotaExceeded
	}
	return nil
}

// ownerLocks da un cerrojo por propietario: serializa solo el tramo corto de
// confirmar una subida (comprobar cuota + mover a destino + registrar), nunca
// la lectura del cuerpo, que es lo lento. El valor cero es utilizable.
//
// Es un cerrojo de proceso: una subida por la CLI a la vez que el servidor,
// para el mismo propietario, podría pasarse de la cuota por un archivo
// (documentado en ADR-036).
type ownerLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *ownerLocks) lock(ownerID string) (unlock func()) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*sync.Mutex)
	}
	m, ok := l.locks[ownerID]
	if !ok {
		m = &sync.Mutex{}
		l.locks[ownerID] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock
}
