package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
	"github.com/porrii/nexuscloud/internal/storage"
)

// Manager es el Backup Manager (§18, ADR-015). No toca la ruta de lectura/
// escritura normal de FileService -- lee a través del mismo ProviderResolver
// que ya usa el resto de internal/storage, nunca el filesystem a pelo.
type Manager struct {
	pools     storage.PoolRepository
	files     storage.FileRepository
	providers storage.ProviderResolver
	repo      Repository
}

func NewManager(pools storage.PoolRepository, files storage.FileRepository, providers storage.ProviderResolver, repo Repository) *Manager {
	return &Manager{pools: pools, files: files, providers: providers, repo: repo}
}

type RunOptions struct {
	// PoolIDs son IDs de pool ya resueltos (nunca nombres -- eso es
	// responsabilidad de quien llama, p.ej. internal/cli/backup_cmd.go vía
	// el mismo resolvePoolRef que ya usan los comandos "storage pool").
	// Vacío = todos los pools activos elegibles.
	PoolIDs         []string
	DestinationPath string
}

// Run ejecuta un backup manual y completo. Un pool con BackupPolicy=off
// queda excluido siempre, se haya pedido explícitamente en PoolIDs o no --
// "off" es una señal más fuerte que "me lo han pedido por nombre" (decisión
// confirmada, ADR-015). inherit/on se incluyen ambos.
//
// Si cualquier fichero falla (lectura, escritura, hash que no coincide), el
// job entero se marca failed con el fichero exacto en el error -- nunca se
// escribe manifest.json a medias, así que Restore jamás puede operar sobre
// un backup incompleto (no lee ningún estado hasta encontrar el manifiesto).
// La carpeta parcial en disco NO se borra al fallar: queda como evidencia
// para depurar la causa, sin comprometer esa invariante de seguridad.
func (m *Manager) Run(ctx context.Context, opts RunOptions) (*Job, error) {
	candidatePools, err := m.resolveCandidatePools(ctx, opts.PoolIDs)
	if err != nil {
		return nil, err
	}

	job := &Job{
		ID:              idgen.New(),
		Status:          StatusRunning,
		DestinationPath: opts.DestinationPath,
		PoolIDs:         poolIDsOf(candidatePools),
		StartedAt:       time.Now().UTC(),
	}
	if err := m.repo.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("creando backup job: %w", err)
	}

	jobDir := filepath.Join(opts.DestinationPath, job.ID)
	dataDir := filepath.Join(jobDir, "data")
	manifest := &Manifest{JobID: job.ID, CreatedAt: job.StartedAt}

	for _, pool := range candidatePools {
		provider, err := m.providers.For(ctx, pool.ID)
		if err != nil {
			return m.fail(ctx, job, fmt.Errorf("resolviendo el almacenamiento del pool %q: %w", pool.Name, err))
		}
		activeFiles, err := m.files.ListFilesByPool(ctx, pool.ID)
		if err != nil {
			return m.fail(ctx, job, fmt.Errorf("listando archivos del pool %q: %w", pool.Name, err))
		}

		poolManifest := PoolManifest{PoolID: pool.ID, PoolName: pool.Name}
		for _, meta := range activeFiles {
			rc, err := provider.Read(ctx, sourceRelPath(meta.OwnerID, meta.ParentPath, meta.Name))
			if err != nil {
				return m.fail(ctx, job, fmt.Errorf("leyendo %q/%s: %w", pool.Name, logicalPath(meta.ParentPath, meta.Name), err))
			}
			destPath := filepath.Join(dataDir, pool.ID, meta.OwnerID, filepath.FromSlash(meta.ParentPath), meta.Name)
			sha, size, copyErr := copyVerified(rc, destPath, meta.SHA256)
			rc.Close()
			if copyErr != nil {
				return m.fail(ctx, job, fmt.Errorf("respaldando %q/%s: %w", pool.Name, logicalPath(meta.ParentPath, meta.Name), copyErr))
			}
			poolManifest.Files = append(poolManifest.Files, FileManifest{
				OwnerID: meta.OwnerID, ParentPath: meta.ParentPath, Name: meta.Name,
				SizeBytes: size, SHA256: sha,
			})
			job.FileCount++
			job.TotalBytes += size
		}
		manifest.Pools = append(manifest.Pools, poolManifest)
	}

	if err := writeManifest(jobDir, manifest); err != nil {
		return m.fail(ctx, job, err)
	}
	if err := m.repo.FinishJob(ctx, job.ID, StatusCompleted, job.FileCount, job.TotalBytes, ""); err != nil {
		return nil, fmt.Errorf("marcando el backup job como completado: %w", err)
	}
	job.Status = StatusCompleted
	return job, nil
}

func (m *Manager) fail(ctx context.Context, job *Job, cause error) (*Job, error) {
	if err := m.repo.FinishJob(ctx, job.ID, StatusFailed, job.FileCount, job.TotalBytes, cause.Error()); err != nil {
		return nil, fmt.Errorf("%w (y además no se pudo registrar el fallo del job: %v)", cause, err)
	}
	return nil, cause
}

func (m *Manager) resolveCandidatePools(ctx context.Context, poolIDs []string) ([]*storage.Pool, error) {
	var base []*storage.Pool
	if len(poolIDs) == 0 {
		all, err := m.pools.ListPools(ctx)
		if err != nil {
			return nil, fmt.Errorf("listando storage pools: %w", err)
		}
		base = all
	} else {
		for _, id := range poolIDs {
			p, err := m.pools.GetPoolByID(ctx, id)
			if err != nil {
				return nil, err
			}
			base = append(base, p)
		}
	}

	var eligible []*storage.Pool
	for _, p := range base {
		if p.Status != "active" || p.BackupPolicy == storage.PolicyOff {
			continue
		}
		eligible = append(eligible, p)
	}
	if len(eligible) == 0 {
		return nil, ErrNoEligiblePools
	}
	return eligible, nil
}

func poolIDsOf(pools []*storage.Pool) []string {
	ids := make([]string, len(pools))
	for i, p := range pools {
		ids[i] = p.ID
	}
	return ids
}

// sourceRelPath calca deliberadamente path.Join("/", ownerID, parentPath,
// name) -- el mismo aislamiento por propietario que physicalPath en
// internal/storage/file_service.go (no exportado: es un único path.Join sin
// lógica propia, así que duplicar esta línea es más quirúrgico que exportar
// y tocar sus ~12 puntos de uso existentes). Si esa convención cambiara
// alguna vez, actualizar las dos a la vez.
func sourceRelPath(ownerID, parentPath, name string) string {
	return path.Join("/", ownerID, parentPath, name)
}

// logicalPath junta parentPath+name para mensajes de error legibles (evita
// dobles barras cuando parentPath es "/", el caso más común).
func logicalPath(parentPath, name string) string {
	return path.Join(parentPath, name)
}

// List devuelve todos los backups, más reciente primero.
func (m *Manager) List(ctx context.Context) ([]*Job, error) {
	return m.repo.ListJobs(ctx)
}

type RestoreOptions struct {
	JobID           string
	DestinationPath string
}

// Restore extrae los ficheros de un backup a una carpeta elegida,
// re-verificando cada SHA-256 contra el manifiesto antes de darlo por
// recuperado (defensa en profundidad: un bitrot en el propio disco de
// backup se detecta en vez de restaurar algo corrupto en silencio). No
// reinserta nada en un pool activo ni toca la base de datos -- alcance
// explícito de este slice, ver ADR-015.
func (m *Manager) Restore(ctx context.Context, opts RestoreOptions) error {
	job, err := m.repo.GetJobByID(ctx, opts.JobID)
	if err != nil {
		return err
	}
	if job.Status != StatusCompleted {
		return fmt.Errorf("%w: %s (status=%s)", ErrJobNotRestorable, job.ID, job.Status)
	}

	jobDir := filepath.Join(job.DestinationPath, job.ID)
	manifest, err := readManifest(jobDir)
	if err != nil {
		return fmt.Errorf("leyendo el manifiesto del backup %s: %w", job.ID, err)
	}
	dataDir := filepath.Join(jobDir, "data")

	// A diferencia de Run (que aborta el job entero ante el primer fallo,
	// porque un backup a medias jamás debe parecer completo), aquí se
	// restauran todos los ficheros que se puedan: un solo fichero con
	// bitrot en el disco de backup no debe privar al usuario de recuperar
	// el resto. Los fallos se acumulan y se devuelven juntos al final --
	// nunca en silencio (plan de Fase 5 slice 1).
	var errs []error
	for _, pm := range manifest.Pools {
		for _, fm := range pm.Files {
			srcPath := filepath.Join(dataDir, pm.PoolID, fm.OwnerID, filepath.FromSlash(fm.ParentPath), fm.Name)
			destPath := filepath.Join(opts.DestinationPath, pm.PoolID, fm.OwnerID, filepath.FromSlash(fm.ParentPath), fm.Name)
			if err := restoreOneFile(srcPath, destPath, fm.SHA256); err != nil {
				errs = append(errs, fmt.Errorf("restaurando %q/%s: %w", pm.PoolName, logicalPath(fm.ParentPath, fm.Name), err))
			}
		}
	}
	return errors.Join(errs...)
}

func restoreOneFile(srcPath, destPath, expectedSHA256 string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("abriendo la copia del backup: %w", err)
	}
	defer f.Close()
	_, _, err = copyVerified(f, destPath, expectedSHA256)
	return err
}

// copyVerified copia r a destPath (creando los directorios que hagan
// falta) calculando su SHA-256 mientras escribe. Si no coincide con
// expectedSHA256, borra destPath en vez de dejar un fichero corrupto a
// medio escribir en el backup/restauración -- nunca se da por buena una
// copia sin verificar.
func copyVerified(r io.Reader, destPath, expectedSHA256 string) (sha256Hex string, size int64, err error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return "", 0, fmt.Errorf("creando el directorio destino: %w", err)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("creando %s: %w", destPath, err)
	}
	hasher := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hasher), r)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return "", 0, fmt.Errorf("copiando el contenido: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return "", 0, fmt.Errorf("cerrando %s: %w", destPath, closeErr)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA256 {
		os.Remove(destPath)
		return "", 0, fmt.Errorf("%w: esperado %s, obtenido %s", ErrIntegrityMismatch, expectedSHA256, got)
	}
	return got, n, nil
}
