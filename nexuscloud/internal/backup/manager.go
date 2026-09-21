package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/porrii/nexuscloud/internal/config"
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
	// fileSvc solo lo usa RestoreToPool (ADR-025): reinsertar en un pool
	// activo tiene que pasar por FileService.Upload/Mkdir -- el único punto
	// de acceso a archivos de usuario -- para heredar validación de
	// nombre/ruta, el rechazo si el path está ocupado por la papelera, y el
	// versionado automático (ADR-007), en vez de reimplementar esa lógica
	// aquí escribiendo el filesystem/la BD a pelo.
	fileSvc *storage.FileService
	// argon2Params (ADR-028) son los MISMOS parámetros de coste ya
	// configurados para hashing de contraseñas (cfg.Security.Argon2,
	// internal/auth.NewHasher) -- se reutilizan tal cual como KDF para
	// convertir una passphrase de backup en una clave AES-256, sin
	// introducir una config de coste nueva y separada.
	argon2Params config.Argon2Config
}

func NewManager(pools storage.PoolRepository, files storage.FileRepository, providers storage.ProviderResolver, repo Repository, fileSvc *storage.FileService, argon2Params config.Argon2Config) *Manager {
	return &Manager{pools: pools, files: files, providers: providers, repo: repo, fileSvc: fileSvc, argon2Params: argon2Params}
}

// deriveBackupKey (ADR-028) convierte una passphrase de backup en una clave
// AES-256 de 32 bytes vía Argon2id -- mismo algoritmo y mismos parámetros
// de coste que el hashing de contraseñas de usuario, con un salt propio
// (aleatorio, uno por job, nunca compartido con el de ninguna contraseña).
func (m *Manager) deriveBackupKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, m.argon2Params.Iterations, m.argon2Params.MemoryKiB, m.argon2Params.Parallelism, 32)
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generando bytes aleatorios: %w", err)
	}
	return b, nil
}

// resolveDecryptionKey (ADR-028) centraliza lo que Restore/RestoreToPool/
// Verify necesitan por igual: si el manifiesto no está cifrado, no hace
// falta ninguna clave (nil, sin importar si se dio una passphrase -- no es
// un error pasar una passphrase de más). Si SÍ está cifrado, exige una
// passphrase no vacía y deriva la clave con el salt de ESE job concreto.
func (m *Manager) resolveDecryptionKey(manifest *Manifest, passphrase string) ([]byte, error) {
	if !manifest.Encrypted {
		return nil, nil
	}
	if passphrase == "" {
		return nil, ErrPassphraseRequired
	}
	salt, err := base64.StdEncoding.DecodeString(manifest.Salt)
	if err != nil {
		return nil, fmt.Errorf("decodificando el salt del manifiesto: %w", err)
	}
	return m.deriveBackupKey(passphrase, salt), nil
}

// decryptingReader envuelve r en un cipher.StreamReader si key no es nil --
// AES-CTR es simétrico: "descifrar" es aplicar el mismo keystream que
// "cifrar". Con key nil, devuelve r tal cual, comportamiento actual
// intacto para backups sin cifrar.
func decryptingReader(r io.Reader, key []byte, ivB64 string) (io.Reader, error) {
	if key == nil {
		return r, nil
	}
	iv, err := base64.StdEncoding.DecodeString(ivB64)
	if err != nil {
		return nil, fmt.Errorf("decodificando el IV del fichero: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("inicializando descifrado: %w", err)
	}
	return &cipher.StreamReader{S: cipher.NewCTR(block, iv), R: r}, nil
}

type RunOptions struct {
	// PoolIDs son IDs de pool ya resueltos (nunca nombres -- eso es
	// responsabilidad de quien llama, p.ej. internal/cli/backup_cmd.go vía
	// el mismo resolvePoolRef que ya usan los comandos "storage pool").
	// Vacío = todos los pools activos elegibles.
	PoolIDs         []string
	DestinationPath string
	// RetentionCount/RetentionDays, si son > 0, podan backups COMPLETADOS en
	// este MISMO DestinationPath tras completar este Run con éxito (nunca
	// cuentan ni tocan backups de otra ruta ni jobs failed). Se componen
	// como "unión de motivos para conservar": un backup se poda solo si
	// CADA política activa (>0) vota podarlo -- si cualquiera de las dos
	// activas vota conservarlo, se conserva. Con ambas en 0 (por defecto),
	// nunca se poda nada. Ver ADR-017.
	RetentionCount int
	RetentionDays  int
	// Incremental, si es true, enlaza (hardlink) al backup completado más
	// reciente en este mismo DestinationPath cualquier fichero cuyo
	// SHA-256 no haya cambiado, en vez de recopiarlo -- ver ADR-026. Sin
	// ningún backup anterior en ese destino, se comporta como uno
	// completo normal.
	Incremental bool
	// Encrypt (ADR-028): cifra cada fichero con AES-256-CTR, clave
	// derivada de Passphrase (obligatoria si Encrypt=true, se falla ANTES
	// de crear nada si está vacía) vía Argon2id con un salt nuevo por job.
	// Encrypt=true desactiva el hardlink de Incremental para ESTE run (ver
	// ADR-028): cada job cifrado usa una clave distinta, así que el
	// ciphertext de "el mismo fichero" nunca coincide entre jobs aunque el
	// contenido y la passphrase sean iguales.
	Encrypt    bool
	Passphrase string
	// RemoteToken (ADR-029): obligatorio si DestinationPath es una URL
	// http(s):// -- ignorado sin más para un destino local. Nunca se lee
	// del entorno dentro de este paquete (mismo criterio que Passphrase):
	// el llamador (CLI/servidor) lo obtiene de NEXUSCLOUD_BACKUP_REMOTE_TOKEN.
	RemoteToken string
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
	// Falla ANTES de crear ningún job/directorio si se pidió cifrar sin
	// passphrase -- ADR-028, mismo criterio que "restore-to-pool" fallando
	// antes de tocar nada con un pool inactivo (ADR-025).
	if opts.Encrypt && opts.Passphrase == "" {
		return nil, ErrPassphraseRequired
	}

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

	// dest (ADR-029) abstrae dónde vive este backup -- local o, si
	// DestinationPath es una URL http(s)://, otro servidor NexusCloud por
	// red. Ni jobDir ni dataDir existen ya como rutas de filesystem: todo
	// se expresa como relPath lógico ("manifest.json", "data/<pool>/...")
	// que dest sabe traducir a lo que corresponda.
	dest := resolveDestination(opts.DestinationPath, opts.RemoteToken)
	manifest := &Manifest{JobID: job.ID, CreatedAt: job.StartedAt}

	// Cifrado (ADR-028): salt nuevo por job, clave derivada una vez y
	// reutilizada para todos los ficheros de este run -- cada fichero
	// tendrá su propio IV (generado dentro de copyVerified), pero la
	// clave AES-256 es la misma para todo el job.
	var encKey []byte
	if opts.Encrypt {
		salt, err := randomBytes(16)
		if err != nil {
			return m.fail(ctx, job, fmt.Errorf("generando el salt de cifrado: %w", err))
		}
		encKey = m.deriveBackupKey(opts.Passphrase, salt)
		manifest.Encrypted = true
		manifest.Salt = base64.StdEncoding.EncodeToString(salt)
	}

	// Backup incremental (ADR-026): localiza el backup completado más
	// reciente en este mismo destino y su manifiesto, para poder enlazar
	// (en vez de recopiar) cualquier fichero cuyo SHA-256 no haya
	// cambiado. Sin ningún backup anterior aquí, o si su manifiesto no se
	// puede leer, previous queda nil -- ningún fichero coincidirá nunca
	// con un mapa nil, así que esto degrada a un backup completo normal
	// sin necesitar una rama aparte para "primera vez". Con Encrypt=true,
	// previous se deja sin construir a propósito (ver RunOptions.Encrypt):
	// la comprobación "if previous != nil" de más abajo hace que ningún
	// fichero intente enlazarse, reutilizando la propia rama de "sin
	// backup anterior" en vez de una segunda condición dentro del bucle.
	var previous map[string]string // poolID+"|"+ownerID+"|"+parentPath+"|"+name -> sha256
	var prevJobID string
	if opts.Incremental && !opts.Encrypt {
		if completed, err := m.completedJobsAtDestination(ctx, opts.DestinationPath); err == nil && len(completed) > 0 {
			prevJobID = completed[0].ID
			if prevManifest, err := readManifest(ctx, dest, prevJobID); err == nil {
				previous = make(map[string]string)
				for _, pm := range prevManifest.Pools {
					for _, fm := range pm.Files {
						previous[pm.PoolID+"|"+fm.OwnerID+"|"+fm.ParentPath+"|"+fm.Name] = fm.SHA256
					}
				}
			}
		}
	}

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
			relPath := path.Join("data", pool.ID, meta.OwnerID, meta.ParentPath, meta.Name)

			if previous != nil {
				key := pool.ID + "|" + meta.OwnerID + "|" + meta.ParentPath + "|" + meta.Name
				if prevSHA, found := previous[key]; found && prevSHA == meta.SHA256 {
					if dest.Link(ctx, prevJobID, job.ID, relPath) == nil {
						poolManifest.Files = append(poolManifest.Files, FileManifest{
							OwnerID: meta.OwnerID, ParentPath: meta.ParentPath, Name: meta.Name,
							SizeBytes: meta.SizeBytes, SHA256: meta.SHA256,
						})
						job.FileCount++
						job.TotalBytes += meta.SizeBytes
						continue
					}
					// El enlace no se pudo hacer (destino remoto --
					// siempre, ver ErrLinkUnsupported --, otro
					// filesystem, el backup anterior ya no está,
					// etc.) -- se cae a la copia normal de abajo,
					// nunca se aborta el job por esto: la
					// incrementalidad es una optimización, no una
					// condición de éxito.
				}
			}

			rc, err := provider.Read(ctx, sourceRelPath(meta.OwnerID, meta.ParentPath, meta.Name))
			if err != nil {
				return m.fail(ctx, job, fmt.Errorf("leyendo %q/%s: %w", pool.Name, logicalPath(meta.ParentPath, meta.Name), err))
			}
			sha, size, iv, copyErr := copyToDestination(ctx, dest, job.ID, relPath, rc, meta.SHA256, encKey)
			rc.Close()
			if copyErr != nil {
				return m.fail(ctx, job, fmt.Errorf("respaldando %q/%s: %w", pool.Name, logicalPath(meta.ParentPath, meta.Name), copyErr))
			}
			poolManifest.Files = append(poolManifest.Files, FileManifest{
				OwnerID: meta.OwnerID, ParentPath: meta.ParentPath, Name: meta.Name,
				SizeBytes: size, SHA256: sha, IV: iv,
			})
			job.FileCount++
			job.TotalBytes += size
		}
		manifest.Pools = append(manifest.Pools, poolManifest)
	}

	if err := writeManifest(ctx, dest, job.ID, manifest); err != nil {
		return m.fail(ctx, job, err)
	}
	// ADR-029: el receptor remoto no crea su propia fila en backup_jobs
	// hasta que no confirmamos que TODO (ficheros + manifiesto) se subió
	// con éxito -- así un corte de red a medias deja como mucho una
	// carpeta huérfana en el remoto, nunca un job a medias que parezca
	// completado (mismo invariante que ya sostiene ADR-015 localmente:
	// "backup list" lee de la BD, nunca del disco).
	if remote, ok := dest.(*remoteDestination); ok {
		if err := remote.finalize(ctx, job.ID); err != nil {
			return m.fail(ctx, job, fmt.Errorf("finalizando el backup en el destino remoto: %w", err))
		}
	}
	if err := m.repo.FinishJob(ctx, job.ID, StatusCompleted, job.FileCount, job.TotalBytes, ""); err != nil {
		return nil, fmt.Errorf("marcando el backup job como completado: %w", err)
	}
	job.Status = StatusCompleted

	if opts.RetentionCount > 0 || opts.RetentionDays > 0 {
		m.pruneOldBackups(ctx, opts.DestinationPath, opts.RemoteToken, opts.RetentionCount, opts.RetentionDays)
	}
	return job, nil
}

// pruneOldBackups aplica la política de retención (ADR-017) sobre los
// backups completados en destinationPath. Best-effort y deliberadamente
// silencioso ante errores individuales (§173: el ÉXITO del backup que se
// acaba de completar nunca debe reportarse como fallo solo porque podar
// uno viejo no funcionó). Nunca toca jobs failed (no son "backups sanos"
// que rotar) ni los de otro destino.
//
// Composición de retentionCount/retentionDays: "unión de motivos para
// conservar" -- un backup se poda solo si TODAS las políticas activas
// (>0) votan podarlo; una política en 0 no vota (ni a favor ni en
// contra), simplemente no participa. Así, si solo una está activa, se
// comporta exactamente como esa única política; si las dos lo están, basta
// con que UNA quiera conservarlo para que sobreviva -- nunca al revés.
// completedJobsAtDestination devuelve los backups COMPLETADOS en
// destinationPath, más reciente primero (ListJobs ya viene started_at
// DESC) -- usado tanto por pruneOldBackups (retención, ADR-017) como por
// Run en modo incremental (ADR-026, toma el más reciente como referencia).
func (m *Manager) completedJobsAtDestination(ctx context.Context, destinationPath string) ([]*Job, error) {
	all, err := m.repo.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	var completed []*Job
	for _, j := range all {
		if j.Status == StatusCompleted && j.DestinationPath == destinationPath {
			completed = append(completed, j)
		}
	}
	return completed, nil
}

func (m *Manager) pruneOldBackups(ctx context.Context, destinationPath, remoteToken string, retentionCount, retentionDays int) {
	completed, err := m.completedJobsAtDestination(ctx, destinationPath)
	if err != nil {
		return
	}
	dest := resolveDestination(destinationPath, remoteToken)

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	for i, old := range completed {
		prune := true
		if retentionCount > 0 {
			prune = prune && i >= retentionCount
		}
		if retentionDays > 0 {
			prune = prune && !old.StartedAt.After(cutoff)
		}
		if !prune {
			continue
		}
		if err := dest.RemoveJob(ctx, old.ID); err != nil {
			continue // no se borra la fila si no se pudo borrar la carpeta -- nunca dejar un puntero a nada
		}
		_ = m.repo.DeleteJob(ctx, old.ID)
	}
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
	// Passphrase (ADR-028): obligatoria si el job es Manifest.Encrypted;
	// ignorada sin más si no lo es (no es un error dar una passphrase de
	// más).
	Passphrase string
	// RemoteToken (ADR-029): obligatorio si el backup vive en un destino
	// remoto (job.DestinationPath es http(s)://); ignorado sin más si es
	// local.
	RemoteToken string
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

	dest := resolveDestination(job.DestinationPath, opts.RemoteToken)
	manifest, err := readManifest(ctx, dest, job.ID)
	if err != nil {
		return fmt.Errorf("leyendo el manifiesto del backup %s: %w", job.ID, err)
	}
	key, err := m.resolveDecryptionKey(manifest, opts.Passphrase)
	if err != nil {
		return err
	}

	// A diferencia de Run (que aborta el job entero ante el primer fallo,
	// porque un backup a medias jamás debe parecer completo), aquí se
	// restauran todos los ficheros que se puedan: un solo fichero con
	// bitrot en el disco de backup no debe privar al usuario de recuperar
	// el resto. Los fallos se acumulan y se devuelven juntos al final --
	// nunca en silencio (plan de Fase 5 slice 1).
	var errs []error
	for _, pm := range manifest.Pools {
		for _, fm := range pm.Files {
			relPath := path.Join("data", pm.PoolID, fm.OwnerID, fm.ParentPath, fm.Name)
			destPath := filepath.Join(opts.DestinationPath, pm.PoolID, fm.OwnerID, filepath.FromSlash(fm.ParentPath), fm.Name)
			if err := restoreOneFile(ctx, dest, job.ID, relPath, destPath, fm.SHA256, key, fm.IV); err != nil {
				errs = append(errs, fmt.Errorf("restaurando %q/%s: %w", pm.PoolName, logicalPath(fm.ParentPath, fm.Name), err))
			}
		}
	}
	return errors.Join(errs...)
}

type RestoreToPoolOptions struct {
	JobID  string
	PoolID string // ya resuelto (id real), igual que RunOptions.PoolIDs
	// Passphrase (ADR-028): obligatoria si el job es Manifest.Encrypted.
	Passphrase string
	// RemoteToken (ADR-029): obligatorio si el backup vive en un destino
	// remoto.
	RemoteToken string
}

// RestoreToPoolResult resume una restauración a pool: cuántos ficheros se
// reinsertaron y qué falló, fichero a fichero.
type RestoreToPoolResult struct {
	Restored int
	Errors   []error
}

// RestoreToPool reinserta los ficheros de un backup como archivos activos
// normales en poolID -- navegables, descargables, versionables -- a
// diferencia de Restore (extrae a una carpeta, sin tocar la base de datos).
// El pool destino es siempre explícito (opts.PoolID), nunca el original del
// manifiesto: ese pool puede ya no existir o estar inactivo, precisamente
// el escenario de desastre que esta función cubre (ADR-025).
//
// Mismo criterio que Restore, no el de Run: se intenta TODO lo que se
// pueda, los fallos se acumulan por fichero en vez de abortar el resto --
// un propietario que ya no existe (viola la FK files.owner_id -> users.id,
// §14) hace fallar solo ese fichero.
func (m *Manager) RestoreToPool(ctx context.Context, opts RestoreToPoolOptions) (*RestoreToPoolResult, error) {
	pool, err := m.pools.GetPoolByID(ctx, opts.PoolID)
	if err != nil {
		return nil, fmt.Errorf("resolviendo el pool destino: %w", err)
	}
	if pool.Status != "active" {
		return nil, fmt.Errorf("%w: %q", ErrPoolNotActive, pool.Name)
	}

	job, err := m.repo.GetJobByID(ctx, opts.JobID)
	if err != nil {
		return nil, err
	}
	if job.Status != StatusCompleted {
		return nil, fmt.Errorf("%w: %s (status=%s)", ErrJobNotRestorable, job.ID, job.Status)
	}

	dest := resolveDestination(job.DestinationPath, opts.RemoteToken)
	manifest, err := readManifest(ctx, dest, job.ID)
	if err != nil {
		return nil, fmt.Errorf("leyendo el manifiesto del backup %s: %w", job.ID, err)
	}
	key, err := m.resolveDecryptionKey(manifest, opts.Passphrase)
	if err != nil {
		return nil, err
	}

	result := &RestoreToPoolResult{}
	materialized := map[string]bool{}
	for _, pm := range manifest.Pools {
		for _, fm := range pm.Files {
			relPath := path.Join("data", pm.PoolID, fm.OwnerID, fm.ParentPath, fm.Name)
			label := fmt.Sprintf("%s/%s", pm.PoolName, logicalPath(fm.ParentPath, fm.Name))

			// Misma paranoia que Restore/Verify: nunca reinsertar en un pool
			// activo un fichero corrupto en el propio disco de backup sin
			// decirlo. A diferencia de copyToDestination (que usa Run),
			// Upload calcula SU PROPIO hash del contenido que le demos --
			// nunca compara contra uno esperado -- así que hay que verificar
			// ANTES de pasárselo, no confiar en que lo note por su cuenta.
			// verifyFileHash ya descifra internamente si key no es nil, así
			// que compara siempre contra el hash del PLAINTEXT.
			if err := verifyFileHash(ctx, dest, job.ID, relPath, fm.SHA256, key, fm.IV); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("%s: %w", label, err))
				continue
			}
			if err := m.materializeAncestors(ctx, opts.PoolID, fm.OwnerID, fm.ParentPath, materialized); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("creando carpetas para %s: %w", label, err))
				continue
			}
			f, err := dest.OpenFile(ctx, job.ID, relPath)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("abriendo la copia de %s: %w", label, err))
				continue
			}
			// Upload tiene que recibir el PLAINTEXT -- calcula su propio
			// hash del stream que le demos y lo guarda tal cual en el pool
			// activo, así que un ciphertext sin descifrar aquí dejaría el
			// archivo restaurado ilegible en el pool destino.
			content, err := decryptingReader(f, key, fm.IV)
			if err != nil {
				f.Close()
				result.Errors = append(result.Errors, fmt.Errorf("descifrando %s: %w", label, err))
				continue
			}
			// SkipQuota (ADR-036): restaurar tras un desastre no debe fallar
			// porque la cuota del propietario haya cambiado desde el backup;
			// si lo restaurado lo deja por encima, solo se bloquean subidas nuevas.
			_, uploadErr := m.fileSvc.Upload(ctx, storage.UploadInput{
				OwnerID: fm.OwnerID, ParentPath: fm.ParentPath, Name: fm.Name, Content: content, PoolID: opts.PoolID,
				SkipQuota: true,
			})
			f.Close()
			if uploadErr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("restaurando %s al pool: %w", label, uploadErr))
				continue
			}
			result.Restored++
		}
	}
	return result, nil
}

// materializeAncestors crea, nivel a nivel, cada carpeta intermedia de
// parentPath que todavía no exista en poolID -- mismo problema y mismo
// criterio que FilesRepository.createDirectory en el cliente Flutter
// (ADR-012 punto 8): FileService.Upload nunca crea filas de directories
// para rutas padre inexistentes, así que sin este paso un fichero
// restaurado en una carpeta anidada existiría en la base de datos pero
// sería invisible navegando carpeta a carpeta desde la raíz.
// FileService.Mkdir ya es idempotente (DO NOTHING en conflicto), así que
// repetir la llamada para una carpeta ya creada por otro fichero de este
// mismo restore es seguro -- done solo evita la llamada redundante.
func (m *Manager) materializeAncestors(ctx context.Context, poolID, ownerID, parentPath string, done map[string]bool) error {
	trimmed := strings.Trim(parentPath, "/")
	if trimmed == "" {
		return nil
	}
	current := "/"
	for _, seg := range strings.Split(trimmed, "/") {
		key := ownerID + ":" + current + seg
		if !done[key] {
			if _, err := m.fileSvc.Mkdir(ctx, ownerID, current, seg, poolID); err != nil {
				return err
			}
			done[key] = true
		}
		current = path.Join(current, seg) + "/"
	}
	return nil
}

// FileVerifyResult es el resultado de re-verificar un fichero concreto de
// un backup ya hecho, sin restaurarlo a ningún sitio.
type FileVerifyResult struct {
	PoolName   string
	OwnerID    string
	ParentPath string
	Name       string
	OK         bool
	Error      string // vacío si OK
}

// VerifyResult es el resultado de Verify sobre un job completo.
type VerifyResult struct {
	JobID string
	OK    bool // true solo si TODOS los ficheros verificaron correctamente
	Files []FileVerifyResult
}

// VerifyOptions agrupa los parámetros de Verify -- mismo criterio que
// RestoreOptions/RestoreToPoolOptions (evita una lista de strings
// posicionales fácil de confundir entre sí).
type VerifyOptions struct {
	JobID      string
	Passphrase string
	// RemoteToken (ADR-029): obligatorio si el backup vive en un destino
	// remoto.
	RemoteToken string
}

// Verify relee el manifiesto de un backup y recalcula el SHA-256 de cada
// fichero ya copiado, sin moverlo ni restaurarlo a ningún sitio -- permite
// comprobar la salud de un backup antiguo (p.ej. antes de confiar en él
// para borrar el original, o tras sospechar de un problema de disco) sin
// pagar el coste de una restauración completa. No modifica nada: ni el
// backup, ni la base de datos, ni ningún destino.
func (m *Manager) Verify(ctx context.Context, opts VerifyOptions) (*VerifyResult, error) {
	job, err := m.repo.GetJobByID(ctx, opts.JobID)
	if err != nil {
		return nil, err
	}
	if job.Status != StatusCompleted {
		return nil, fmt.Errorf("%w: %s (status=%s)", ErrJobNotRestorable, job.ID, job.Status)
	}

	dest := resolveDestination(job.DestinationPath, opts.RemoteToken)
	manifest, err := readManifest(ctx, dest, job.ID)
	if err != nil {
		return nil, fmt.Errorf("leyendo el manifiesto del backup %s: %w", job.ID, err)
	}
	key, err := m.resolveDecryptionKey(manifest, opts.Passphrase)
	if err != nil {
		return nil, err
	}

	result := &VerifyResult{JobID: job.ID, OK: true}
	for _, pm := range manifest.Pools {
		for _, fm := range pm.Files {
			relPath := path.Join("data", pm.PoolID, fm.OwnerID, fm.ParentPath, fm.Name)
			fr := FileVerifyResult{PoolName: pm.PoolName, OwnerID: fm.OwnerID, ParentPath: fm.ParentPath, Name: fm.Name}
			if err := verifyFileHash(ctx, dest, job.ID, relPath, fm.SHA256, key, fm.IV); err != nil {
				fr.Error = err.Error()
				result.OK = false
			} else {
				fr.OK = true
			}
			result.Files = append(result.Files, fr)
		}
	}
	return result, nil
}

// verifyFileHash recalcula el SHA-256 de relPath (bajo jobID, en dest) y
// lo compara con expectedSHA256, sin escribir ni mover nada -- a
// diferencia de copyToDestination/copyToLocalFile, es puramente de
// lectura. key/ivB64 (ADR-028): con key nil (backup sin cifrar), lee el
// fichero tal cual; con key no nil, descifra mientras hashea -- el hash
// comparado siempre es el del PLAINTEXT. dest (ADR-029) puede ser local o
// remoto, indistinguible desde aquí.
func verifyFileHash(ctx context.Context, dest Destination, jobID, relPath, expectedSHA256 string, key []byte, ivB64 string) error {
	f, err := dest.OpenFile(ctx, jobID, relPath)
	if err != nil {
		return fmt.Errorf("abriendo el fichero: %w", err)
	}
	defer f.Close()
	r, err := decryptingReader(f, key, ivB64)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return fmt.Errorf("leyendo el fichero: %w", err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA256 {
		return fmt.Errorf("%w: esperado %s, obtenido %s", ErrIntegrityMismatch, expectedSHA256, got)
	}
	return nil
}

// restoreOneFile descifra (si key no es nil) el fichero de backup
// (relPath bajo jobID, en dest) y lo copia en claro a destPath -- SIEMPRE
// una ruta local (la carpeta de restauración elegida por el usuario):
// restaurar/reinsertar nunca vuelve a cifrar el resultado.
func restoreOneFile(ctx context.Context, dest Destination, jobID, relPath, destPath, expectedSHA256 string, key []byte, ivB64 string) error {
	f, err := dest.OpenFile(ctx, jobID, relPath)
	if err != nil {
		return fmt.Errorf("abriendo la copia del backup: %w", err)
	}
	defer f.Close()
	r, err := decryptingReader(f, key, ivB64)
	if err != nil {
		return err
	}
	_, err = copyToLocalFile(destPath, r, expectedSHA256)
	return err
}

// copyToLocalFile copia r a destPath -- SIEMPRE una ruta local, creando
// los directorios que hagan falta -- calculando su SHA-256 mientras
// escribe. Si no coincide con expectedSHA256, borra destPath en vez de
// dejar un fichero corrupto a medio escribir. Usado solo por
// restoreOneFile: restaurar/reinsertar nunca vuelve a cifrar el
// resultado, así que nunca necesita la rama de cifrado de
// copyToDestination.
func copyToLocalFile(destPath string, r io.Reader, expectedSHA256 string) (size int64, err error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return 0, fmt.Errorf("creando el directorio destino: %w", err)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("creando %s: %w", destPath, err)
	}
	hasher := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hasher), r)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return 0, fmt.Errorf("copiando el contenido: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return 0, fmt.Errorf("cerrando %s: %w", destPath, closeErr)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA256 {
		os.Remove(destPath)
		return 0, fmt.Errorf("%w: esperado %s, obtenido %s", ErrIntegrityMismatch, expectedSHA256, got)
	}
	return n, nil
}

// copyToDestination lee r (el PLAINTEXT de origen) y lo escribe en dest
// bajo jobID/relPath, cifrando sobre la marcha si key no es nil, y
// calcula su SHA-256 -- SIEMPRE el del PLAINTEXT de entrada, cifrado o no
// el resultado escrito (ADR-028). Si el hash no coincide con
// expectedSHA256, borra lo ya escrito en dest en vez de dejarlo dado por
// bueno -- nunca se da por buena una copia sin verificar, cueste lo que
// cueste haber subido de más a un destino remoto (ver ADR-029). Usado
// solo por Run.
//
// key (ADR-028): nil escribe el contenido tal cual. No nil cifra con
// AES-256-CTR y un IV aleatorio nuevo (devuelto en base64 como ivB64,
// para guardarlo en el manifiesto -- (clave, IV) nunca debe repetirse en
// CTR). io.TeeReader alimenta el hasher con el PLAINTEXT tal como sale de
// r, ANTES de que el cipher.StreamReader (si lo hay) lo cifre para
// dest.WriteFile -- Run/Restore/RestoreToPool/Verify no cambian su
// criterio de integridad por el hecho de cifrar.
func copyToDestination(ctx context.Context, dest Destination, jobID, relPath string, r io.Reader, expectedSHA256 string, key []byte) (sha256Hex string, size int64, ivB64 string, err error) {
	hasher := sha256.New()
	teed := io.TeeReader(r, hasher)

	final := io.Reader(teed)
	if key != nil {
		iv, err := randomBytes(aes.BlockSize)
		if err != nil {
			return "", 0, "", err
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return "", 0, "", fmt.Errorf("inicializando cifrado: %w", err)
		}
		final = &cipher.StreamReader{S: cipher.NewCTR(block, iv), R: teed}
		ivB64 = base64.StdEncoding.EncodeToString(iv)
	}

	n, err := dest.WriteFile(ctx, jobID, relPath, final)
	if err != nil {
		return "", 0, "", fmt.Errorf("copiando el contenido: %w", err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA256 {
		_ = dest.RemoveFile(ctx, jobID, relPath)
		return "", 0, "", fmt.Errorf("%w: esperado %s, obtenido %s", ErrIntegrityMismatch, expectedSHA256, got)
	}
	return got, n, ivB64, nil
}
