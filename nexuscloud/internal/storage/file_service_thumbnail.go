package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// thumbnailMaxAttempts: tras este número de intentos fallidos
// consecutivos, un job pasa a status=failed y deja de reintentarse solo
// (§34, ADR-041 Decisión 1).
const thumbnailMaxAttempts = 3

// Timeouts por pipeline (§34, ADR-041 Decisión 4): un límite de
// aislamiento fijo, no una preferencia de administrador -- mismo
// criterio que thumbnailMaxDimension/thumbnailJPEGQuality
// (thumbnail_image.go), por eso no viven en ThumbnailsConfig. Vídeo/PDF
// tardan más que decodificar una imagen en memoria porque son un
// subproceso completo (arranque + demux + decode de un frame).
const (
	DefaultImageThumbnailTimeout = 10 * time.Second
	DefaultVideoThumbnailTimeout = 30 * time.Second
	DefaultPDFThumbnailTimeout   = 20 * time.Second
)

var (
	ErrThumbnailsDisabled = errors.New("storage: la generación de miniaturas está desactivada en este servidor")
	// ErrThumbnailBusy: el semáforo de concurrencia global está lleno --
	// se devuelve de inmediato (§34 Decisión 6, hallazgo ALTO del pase de
	// seguridad), nunca se encola la petición a esperar indefinidamente.
	// El archivo ya tiene su icono genérico como fallback en la web.
	ErrThumbnailBusy = errors.New("storage: demasiadas miniaturas generándose a la vez, inténtalo de nuevo en un momento")
)

// ThumbnailLimits agrupa los límites configurables de las 3 pipelines
// (§34, ADR-041) en un solo struct, para no repartir media docena de
// parámetros sueltos entre WithThumbnails y cada llamada de generación.
type ThumbnailLimits struct {
	Image                    ImageThumbnailLimits
	Video                    ExecThumbnailLimits
	PDF                      ExecThumbnailLimits
	MaxConcurrentGenerations int
}

// WithThumbnails activa la generación de miniaturas (§34, ADR-041).
// tempDir es Layout.Temp -- ffmpeg/pdftoppm necesitan una ruta de archivo
// real (a diferencia del pipeline de imagen, que trabaja directamente
// sobre el io.ReadCloser del Provider), así que el contenido se
// materializa ahí antes de invocarlos. Sin esta opción, FileService.Upload
// no encola ningún job y GenerateOrGetThumbnail devuelve
// ErrThumbnailsDisabled.
func WithThumbnails(jobs ThumbnailJobRepository, cache *ThumbnailCache, tempDir string, limits ThumbnailLimits, enabled bool) FileServiceOption {
	return func(s *FileService) {
		s.thumbnailJobs = jobs
		s.thumbnailCache = cache
		s.thumbnailTempDir = tempDir
		s.thumbnailLimits = limits
		s.thumbnailsEnabled = enabled
		if limits.MaxConcurrentGenerations > 0 {
			s.thumbnailSem = make(chan struct{}, limits.MaxConcurrentGenerations)
		}
	}
}

// enqueueThumbnailJob es best-effort (§34, ADR-041 Decisión 1): un fallo
// aquí nunca debe deshacer ni bloquear una subida ya exitosa, mismo
// patrón exacto que IncrementUploadCount
// (file_service_anonymous_upload.go:112-144). Gateado por
// thumbnailsEnabled -- sin este chequeo, la tabla se llenaría en
// silencio aunque la función entera esté desactivada, rompiendo la
// garantía de "desactivado de verdad" que motiva el default enabled=false
// (hallazgo del pase de security-reviewer sobre el diseño).
func (s *FileService) enqueueThumbnailJob(ctx context.Context, meta *FileMeta) {
	if !s.thumbnailsEnabled || s.thumbnailJobs == nil {
		return
	}
	kind := ThumbnailKindForMimeType(meta.MimeType)
	if kind == "" {
		return
	}
	_, _ = s.thumbnailJobs.UpsertPending(ctx, meta.ID, meta.SHA256, kind)
}

// GenerateOrGetThumbnail resuelve la miniatura de un archivo, generándola
// bajo demanda si no está en caché -- mismo chequeo de propiedad/
// compartición que Download (§34, ADR-041 Decisión 1). Da cobertura
// retroactiva de archivos subidos antes de esta fase (o con
// thumbnails.enabled=false en su momento), y es la fuente de verdad
// correcta incluso si el bucle en segundo plano está desactivado o va
// con retraso. becameFailed indica si ESTE intento agotó los reintentos
// del job -- el llamador (el handler HTTP, que sí tiene AuditLog) decide
// si audita EventThumbnailGenerationFailed; FileService no audita nada
// directamente, mismo criterio que el resto del paquete storage.
func (s *FileService) GenerateOrGetThumbnail(ctx context.Context, requesterID, fileID string) (meta *FileMeta, data []byte, becameFailed bool, err error) {
	if !s.thumbnailsEnabled {
		return nil, nil, false, ErrThumbnailsDisabled
	}
	meta, err = s.files.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, nil, false, err
	}
	if meta.OwnerID != requesterID {
		ok, err := s.hasShareAccessToFile(ctx, requesterID, meta)
		if err != nil {
			return nil, nil, false, err
		}
		if !ok {
			return nil, nil, false, ErrForbidden
		}
	}

	kind := ThumbnailKindForMimeType(meta.MimeType)
	if kind == "" {
		return meta, nil, false, ErrThumbnailUnsupportedFormat
	}

	if cached, found, err := s.thumbnailCache.Get(meta.OwnerID, meta.SHA256); err != nil {
		return meta, nil, false, err
	} else if found {
		return meta, cached, false, nil
	}
	if !s.thumbnailCache.HasRoom() {
		return meta, nil, false, ErrThumbnailCacheFull
	}

	// GetByFileID primero, NUNCA UpsertPending directamente aquí: un job
	// ya en curso (que el bucle en segundo plano podría estar trabajando)
	// no debe perder su cuenta de intentos solo porque alguien también
	// pidió la miniatura bajo demanda al mismo tiempo -- ver el propio
	// comentario de GetByFileID/UpsertPending en thumbnail_job.go.
	job, err := s.thumbnailJobs.GetByFileID(ctx, meta.ID)
	if errors.Is(err, ErrThumbnailJobNotFound) {
		job, err = s.thumbnailJobs.UpsertPending(ctx, meta.ID, meta.SHA256, kind)
	}
	if err != nil {
		return meta, nil, false, fmt.Errorf("preparando job de miniatura: %w", err)
	}

	data, genErr := s.generateAndCacheOnce(ctx, meta, kind)
	if genErr != nil {
		becameFailed, _ = s.thumbnailJobs.MarkFailedAttempt(ctx, job.ID, genErr.Error(), thumbnailMaxAttempts)
		return meta, nil, becameFailed, genErr
	}
	// Best-effort: si esto falla, el job queda pending y el bucle en
	// segundo plano lo recogerá en el siguiente tick -- encontrará la
	// miniatura ya en caché (mismo criterio de idempotencia que el resto
	// del pipeline) y lo marcará done él mismo. No hace fallar la
	// respuesta al cliente, que ya tiene su miniatura en la mano.
	_ = s.thumbnailJobs.MarkDone(ctx, job.ID)
	return meta, data, false, nil
}

// ListThumbnailJobs pagina los jobs de un estado (§34, ADR-041 Decisión 1)
// -- visibilidad administrativa, la razón por la que se eligió una tabla
// persistente en vez de una cola en memoria. A diferencia de
// GenerateOrGetThumbnail, NO depende de thumbnailsEnabled: un
// administrador puede querer ver el rastro que dejaron jobs de cuando la
// función SÍ estaba activada. nil si WithThumbnails nunca se aplicó
// (mismo criterio defensivo que enqueueThumbnailJob).
func (s *FileService) ListThumbnailJobs(ctx context.Context, status ThumbnailJobStatus, limit, offset int) ([]*ThumbnailJob, error) {
	if s.thumbnailJobs == nil {
		return nil, nil
	}
	return s.thumbnailJobs.ListByStatus(ctx, status, limit, offset)
}

// ThumbnailJobResult resume qué pasó al procesar un job (§34, ADR-041
// Decisión 1) -- el llamador (startThumbnailLoop, server.go) decide qué
// loguear/auditar con esta información; FileService no lo hace
// directamente.
type ThumbnailJobResult struct {
	// Processed es false si no había ningún job pendiente, o si había uno
	// pero no se pudo procesar por contención transitoria (caché llena o
	// semáforo ocupado) -- en ambos casos el job sigue pending para un
	// tick futuro, SIN gastar un intento: no es culpa del job.
	Processed bool
	// BecameFailed es true si este intento agotó los reintentos del job.
	BecameFailed bool
	FileID       string
	LastError    string
}

// ProcessNextThumbnailJob toma el job pendiente más antiguo y lo procesa
// -- llamado secuencialmente por el bucle en segundo plano
// (startThumbnailLoop), nunca en paralelo consigo mismo (sin worker pool,
// §34 Decisión 1, mismo criterio que el resto del proyecto no tiene
// ningún patrón de pool de trabajadores).
func (s *FileService) ProcessNextThumbnailJob(ctx context.Context) (ThumbnailJobResult, error) {
	job, err := s.thumbnailJobs.NextPending(ctx)
	if err != nil {
		return ThumbnailJobResult{}, fmt.Errorf("buscando siguiente job de miniatura: %w", err)
	}
	if job == nil {
		return ThumbnailJobResult{}, nil
	}
	if !s.thumbnailCache.HasRoom() {
		// No es un fallo del job -- simplemente no hay sitio todavía en la
		// caché. Se deja pending para reintentar en un tick futuro, sin
		// gastar un intento.
		return ThumbnailJobResult{}, nil
	}

	meta, err := s.files.GetFileByID(ctx, job.FileID)
	if err != nil {
		becameFailed, markErr := s.thumbnailJobs.MarkFailedAttempt(ctx, job.ID, err.Error(), thumbnailMaxAttempts)
		if markErr != nil {
			return ThumbnailJobResult{}, markErr
		}
		return ThumbnailJobResult{Processed: true, BecameFailed: becameFailed, FileID: job.FileID, LastError: err.Error()}, nil
	}

	if _, genErr := s.generateAndCacheOnce(ctx, meta, job.Kind); genErr != nil {
		if errors.Is(genErr, ErrThumbnailBusy) {
			// Contención transitoria contra peticiones bajo demanda -- no
			// es un fallo del job, se reintenta en el siguiente tick.
			return ThumbnailJobResult{}, nil
		}
		becameFailed, markErr := s.thumbnailJobs.MarkFailedAttempt(ctx, job.ID, genErr.Error(), thumbnailMaxAttempts)
		if markErr != nil {
			return ThumbnailJobResult{}, markErr
		}
		return ThumbnailJobResult{Processed: true, BecameFailed: becameFailed, FileID: job.FileID, LastError: genErr.Error()}, nil
	}
	if err := s.thumbnailJobs.MarkDone(ctx, job.ID); err != nil {
		return ThumbnailJobResult{}, fmt.Errorf("marcando job de miniatura como completado: %w", err)
	}
	return ThumbnailJobResult{Processed: true, FileID: job.FileID}, nil
}

// generateAndCacheOnce genera y cachea una miniatura, protegida por
// singleflight (evita generar la misma miniatura dos veces en paralelo si
// el bucle en segundo plano y una petición bajo demanda coinciden sobre
// el mismo archivo -- hallazgo MEDIO del pase de seguridad) y por el
// semáforo global de concurrencia compartido por las 3 pipelines
// (hallazgo ALTO, §34 Decisión 6). Usada tanto por el camino bajo demanda
// como por el bucle en segundo plano -- ambos cuentan contra el mismo
// cupo compartido.
func (s *FileService) generateAndCacheOnce(ctx context.Context, meta *FileMeta, kind ThumbnailKind) ([]byte, error) {
	key := meta.OwnerID + "|" + meta.SHA256
	v, err, _ := s.thumbnailSingle.Do(key, func() (any, error) {
		select {
		case s.thumbnailSem <- struct{}{}:
		default:
			return nil, ErrThumbnailBusy
		}
		defer func() { <-s.thumbnailSem }()

		data, err := s.generateThumbnailBytes(ctx, meta, kind)
		if err != nil {
			return nil, err
		}
		if err := s.thumbnailCache.Put(meta.OwnerID, meta.SHA256, data); err != nil {
			return nil, fmt.Errorf("guardando miniatura en caché: %w", err)
		}
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// generateThumbnailBytes despacha al pipeline que corresponda. El de
// imagen trabaja directamente sobre el io.ReadCloser del Provider; vídeo/
// PDF necesitan una ruta de archivo real (ver materializeLocalCopy).
func (s *FileService) generateThumbnailBytes(ctx context.Context, meta *FileMeta, kind ThumbnailKind) ([]byte, error) {
	prov, err := s.providers.For(ctx, meta.PoolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo proveedor del pool: %w", err)
	}
	rel := physicalPath(meta.OwnerID, meta.ParentPath, meta.Name)

	switch kind {
	case ThumbnailKindImage:
		rc, err := prov.Read(ctx, rel)
		if err != nil {
			return nil, fmt.Errorf("leyendo archivo: %w", err)
		}
		defer rc.Close()
		return GenerateImageThumbnail(ctx, rc, meta.SizeBytes, s.thumbnailLimits.Image)

	case ThumbnailKindVideo, ThumbnailKindPDF:
		localPath, cleanup, err := s.materializeLocalCopy(ctx, prov, rel)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		if kind == ThumbnailKindVideo {
			return GenerateVideoThumbnail(ctx, localPath, s.thumbnailTempDir, meta.SizeBytes, s.thumbnailLimits.Video)
		}
		return GeneratePDFThumbnail(ctx, localPath, s.thumbnailTempDir, meta.SizeBytes, s.thumbnailLimits.PDF)

	default:
		return nil, ErrThumbnailUnsupportedFormat
	}
}

// materializeLocalCopy copia el contenido de rel a un fichero temporal
// real en disco. ffmpeg/pdftoppm son binarios externos que necesitan una
// ruta de archivo de verdad (pdftoppm en concreto no puede leer un PDF
// desde stdin: necesita acceso aleatorio para su tabla xref), a
// diferencia del pipeline de imagen, que trabaja directamente sobre el
// io.ReadCloser que da el Provider. Esto mantiene el pipeline de vídeo/
// PDF funcionando igual sea cual sea el Provider real (local hoy,
// potencialmente remoto en el futuro, §157) sin que este código necesite
// saber si hay o no una ruta física detrás.
func (s *FileService) materializeLocalCopy(ctx context.Context, prov Provider, rel string) (path string, cleanup func(), err error) {
	rc, err := prov.Read(ctx, rel)
	if err != nil {
		return "", nil, fmt.Errorf("leyendo archivo: %w", err)
	}
	defer rc.Close()

	tmp, err := os.CreateTemp(s.thumbnailTempDir, "thumb-src-*")
	if err != nil {
		return "", nil, fmt.Errorf("creando copia temporal: %w", err)
	}
	cleanup = func() { os.Remove(tmp.Name()) }

	if _, err := io.Copy(tmp, rc); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, fmt.Errorf("copiando contenido a temporal: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("cerrando copia temporal: %w", err)
	}
	return tmp.Name(), cleanup, nil
}
