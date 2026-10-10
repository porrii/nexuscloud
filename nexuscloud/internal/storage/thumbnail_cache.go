package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// ErrThumbnailCacheFull: se alcanzó thumbnails.maxCacheBytes -- las
// miniaturas ya generadas se siguen sirviendo, pero no se genera ninguna
// más hasta liberar espacio (§34, ADR-041 Decisión 3). Sin eviction LRU en
// esta ronda -- límite conocido, documentado explícitamente en el ADR.
var ErrThumbnailCacheFull = errors.New("storage: la caché de miniaturas alcanzó su tamaño máximo")

// ThumbnailCache es la caché en disco de miniaturas, direccionada por
// contenido Y PARTICIONADA POR PROPIETARIO (§34, ADR-041 Decisión 2):
// <baseDir>/<ownerID>/<sha256[0:2]>/<sha256>.jpg.
//
// La partición por propietario (en vez de deduplicar también ENTRE
// propietarios distintos, como se bocetó originalmente) cierra un oráculo
// de temporización entre inquilinos que señaló el pase de
// security-reviewer sobre el diseño: sin ella, alguien que ya conoce los
// bytes exactos de un archivo podría medir por la velocidad de respuesta
// si otro usuario ya subió ese mismo contenido -- un canal cruzado nuevo
// que no existe en ningún otro sitio del proyecto (OwnerID se comprueba
// en cada acceso, sin excepciones). Con la partición, dos archivos de un
// MISMO propietario con bytes idénticos siguen compartiendo una sola
// miniatura -- la deduplicación DENTRO de un usuario no se pierde.
type ThumbnailCache struct {
	baseDir      string
	maxBytes     int64
	currentBytes atomic.Int64
}

// NewThumbnailCache recorre baseDir UNA VEZ para inicializar el contador
// de tamaño total -- se llama al arrancar el servidor, justo después de
// que storage.NewLayout haya resuelto/creado el directorio.
func NewThumbnailCache(baseDir string, maxBytes int64) (*ThumbnailCache, error) {
	c := &ThumbnailCache{baseDir: baseDir, maxBytes: maxBytes}
	var total int64
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("calculando el tamaño inicial de la caché de miniaturas: %w", err)
	}
	c.currentBytes.Store(total)
	return c, nil
}

// HasRoom informa si todavía hay espacio para generar una miniatura más.
// Se comprueba ANTES de decodificar/invocar un subproceso -- no tiene
// sentido malgastar ese trabajo si de todas formas no se va a poder
// guardar el resultado.
func (c *ThumbnailCache) HasRoom() bool {
	return c.currentBytes.Load() < c.maxBytes
}

func (c *ThumbnailCache) path(ownerID, sha256Hex string) string {
	shard := sha256Hex
	if len(shard) > 2 {
		shard = sha256Hex[:2]
	}
	return filepath.Join(c.baseDir, ownerID, shard, sha256Hex+".jpg")
}

// Get devuelve la miniatura ya cacheada de ese propietario, si existe.
func (c *ThumbnailCache) Get(ownerID, sha256Hex string) (data []byte, found bool, err error) {
	data, err = os.ReadFile(c.path(ownerID, sha256Hex))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("leyendo miniatura cacheada: %w", err)
	}
	return data, true, nil
}

// Put escribe una miniatura de forma ATÓMICA: fichero temporal + rename
// (mismo patrón staging→move que FileService.Upload,
// file_service.go:228-267) -- dos peticiones simultáneas generando la
// misma miniatura nunca entrelazan bytes en el destino final (hallazgo
// MEDIO del pase de seguridad sobre el diseño: escritura de caché no
// atómica).
func (c *ThumbnailCache) Put(ownerID, sha256Hex string, data []byte) error {
	dest := c.path(ownerID, sha256Hex)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("creando el directorio de la caché de miniaturas: %w", err)
	}
	tmp := dest + ".tmp." + idgen.New()
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("escribiendo miniatura temporal: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		// A diferencia de POSIX (rename atómico, reemplaza aunque el
		// destino esté abierto), en Windows renombrar ENCIMA de un destino
		// que otra goroutine está renombrando en ese mismo instante puede
		// devolver "Access is denied" en vez de reemplazar sin más --
		// confirmado de verdad por `go test -race` en Windows CI con 20
		// goroutines escribiendo la MISMA clave a la vez (nunca reprodujo
		// en Linux). No es una corrupción: es una caché direccionada por
		// contenido, así que si el destino YA EXISTE cuando llegamos aquí,
		// alguien más ganó la carrera escribiendo un valor válido para esta
		// misma clave -- no hace falta el nuestro. Solo se trata como éxito
		// en ese caso concreto (dest existe de verdad); cualquier otro
		// motivo de fallo se sigue reportando tal cual.
		if _, statErr := os.Stat(dest); statErr == nil {
			return nil
		}
		return fmt.Errorf("moviendo miniatura a su destino final: %w", err)
	}
	c.currentBytes.Add(int64(len(data)))
	return nil
}
