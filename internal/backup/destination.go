package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/porrii/nexuscloud/internal/storage"
)

// ErrLinkUnsupported (ADR-029) lo devuelve cualquier Destination que no
// pueda enlazar (hardlink) un fichero de un job anterior -- siempre el
// caso de un destino remoto, nunca un error real: Run ya sabe caer a una
// copia completa cuando el enlace falla (ADR-026), sea cual sea la razón.
var ErrLinkUnsupported = errors.New("backup: este destino no soporta enlazar ficheros entre backups")

// Destination (ADR-029) abstrae DÓNDE viven físicamente los bytes de un
// backup -- una carpeta local (de siempre) o, desde este ADR, otro
// servidor NexusCloud por red. Run/Restore/RestoreToPool/Verify/
// pruneOldBackups nunca tocan os.*/filepath.* directamente sobre el
// destino: todo pasa por aquí, así que ninguno de los cuatro necesita
// saber si el destino de un job concreto es local o remoto.
//
// relPath es siempre una ruta lógica con "/" (nunca separadores nativos
// del SO, viaja tal cual como sufijo de URL en el caso remoto): o bien
// "manifest.json", o bien "data/<pool-id>/<owner-id>/<ruta>/<nombre>" --
// misma convención que ya usaba jobDir antes de este ADR.
type Destination interface {
	// WriteFile escribe relPath bajo jobID, devolviendo el tamaño escrito.
	WriteFile(ctx context.Context, jobID, relPath string, r io.Reader) (size int64, err error)
	OpenFile(ctx context.Context, jobID, relPath string) (io.ReadCloser, error)
	// RemoveFile borra un único fichero de un job -- usado cuando su hash
	// no verifica, para no dejar nunca un fichero corrupto dado por bueno
	// (mismo criterio que copyVerified ya aplicaba antes de este ADR).
	RemoveFile(ctx context.Context, jobID, relPath string) error
	// RemoveJob borra TODO lo asociado a jobID (retención, ADR-017).
	RemoveJob(ctx context.Context, jobID string) error
	// Link intenta enlazar (no copiar) relPath de prevJobID como relPath
	// de jobID -- optimización de espacio (ADR-026), nunca una condición
	// de éxito: quien llama ya sabe caer a copia completa si esto falla.
	Link(ctx context.Context, prevJobID, jobID, relPath string) error
}

// resolveDestination decide local vs remoto mirando el propio destPath --
// "http://"/"https://" es remoto, cualquier otra cosa se trata como
// carpeta local (comportamiento de siempre). Barato de construir en los
// dos casos: no hay estado que compartir entre llamadas.
func resolveDestination(destPath, remoteToken string) Destination {
	if strings.HasPrefix(destPath, "http://") || strings.HasPrefix(destPath, "https://") {
		return &remoteDestination{baseURL: strings.TrimSuffix(destPath, "/"), token: remoteToken, client: http.DefaultClient}
	}
	return &localDestination{root: destPath}
}

// localDestination envuelve la lógica de os.*/filepath.* que ya existía
// antes de ADR-029 (refactor puramente mecánico, cero cambio de
// comportamiento frente a lo que había) -- con una diferencia deliberada:
// path() pasa por storage.SafeJoin (el mismo aislamiento que ya usa todo
// internal/storage para archivos de usuario) en vez de un filepath.Join a
// pelo. Antes de ADR-029, jobID/relPath siempre venían de código interno
// de confianza (idgen.New(), metadatos ya validados en la BD); desde
// ADR-029, este mismo tipo puede quedar detrás de un endpoint HTTP
// autenticado solo por un token compartido (ver internal/api/v1) --
// SafeJoin hace que esta protección exista una sola vez, para cualquier
// llamador, en vez de depender de que cada llamador futuro recuerde
// añadirla por su cuenta.
type localDestination struct {
	root string
}

func (d *localDestination) path(jobID, relPath string) (string, error) {
	return storage.SafeJoin(d.root, jobID+"/"+relPath)
}

func (d *localDestination) WriteFile(ctx context.Context, jobID, relPath string, r io.Reader) (int64, error) {
	dest, err := d.path(jobID, relPath)
	if err != nil {
		return 0, fmt.Errorf("ruta de backup inválida: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return 0, fmt.Errorf("creando el directorio destino: %w", err)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("creando %s: %w", dest, err)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(dest)
		return 0, fmt.Errorf("copiando el contenido: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(dest)
		return 0, fmt.Errorf("cerrando %s: %w", dest, closeErr)
	}
	return n, nil
}

func (d *localDestination) OpenFile(ctx context.Context, jobID, relPath string) (io.ReadCloser, error) {
	dest, err := d.path(jobID, relPath)
	if err != nil {
		return nil, fmt.Errorf("ruta de backup inválida: %w", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		return nil, fmt.Errorf("abriendo %s: %w", relPath, err)
	}
	return f, nil
}

func (d *localDestination) RemoveFile(ctx context.Context, jobID, relPath string) error {
	dest, err := d.path(jobID, relPath)
	if err != nil {
		return fmt.Errorf("ruta de backup inválida: %w", err)
	}
	return os.Remove(dest)
}

func (d *localDestination) RemoveJob(ctx context.Context, jobID string) error {
	dir, err := storage.SafeJoin(d.root, jobID)
	if err != nil {
		return fmt.Errorf("job id inválido: %w", err)
	}
	return os.RemoveAll(dir)
}

func (d *localDestination) Link(ctx context.Context, prevJobID, jobID, relPath string) error {
	dest, err := d.path(jobID, relPath)
	if err != nil {
		return fmt.Errorf("ruta de backup inválida: %w", err)
	}
	src, err := d.path(prevJobID, relPath)
	if err != nil {
		return fmt.Errorf("ruta de backup inválida: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	return os.Link(src, dest)
}

// NewLocalDestination expone localDestination fuera del paquete -- lo usa
// internal/api/v1 para que el servidor RECEPTOR de un backup remoto
// (ADR-029) escriba/lea/borre exactamente con la misma lógica (y la misma
// protección SafeJoin) que un backup hecho localmente, en vez de
// reimplementar el manejo de ficheros en la capa HTTP.
func NewLocalDestination(root string) Destination {
	return &localDestination{root: root}
}

// remoteDestination (ADR-029) habla con otro servidor NexusCloud por
// HTTP, autenticado con un token compartido dedicado (nunca una sesión de
// usuario -- ver ADR-029 sobre por qué). No reintenta nada: un fallo de
// red aborta el job igual que hoy aborta un fallo de escritura local
// (ADR-015, todo o nada).
type remoteDestination struct {
	baseURL string
	token   string
	client  *http.Client
}

func (d *remoteDestination) fileURL(jobID, relPath string) string {
	return fmt.Sprintf("%s/api/v1/backups/inbound/%s/files/%s", d.baseURL, jobID, relPath)
}

func (d *remoteDestination) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-NexusCloud-Backup-Token", d.token)
	return d.client.Do(req)
}

// remoteErr construye un error legible a partir de una respuesta HTTP no
// esperada, incluyendo un fragmento del cuerpo (p.ej. el mensaje JSON de
// error del receptor) sin arriesgarse a volcar una respuesta enorme.
func remoteErr(action string, resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("el destino remoto rechazó %s: %s: %s", action, resp.Status, strings.TrimSpace(string(body)))
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (d *remoteDestination) WriteFile(ctx context.Context, jobID, relPath string, r io.Reader) (int64, error) {
	counted := &countingReader{r: r}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, d.fileURL(jobID, relPath), counted)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := d.do(req)
	if err != nil {
		return 0, fmt.Errorf("subiendo %s al destino remoto: %w", relPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, remoteErr("la subida de "+relPath, resp)
	}
	return counted.n, nil
}

func (d *remoteDestination) OpenFile(ctx context.Context, jobID, relPath string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.fileURL(jobID, relPath), nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.do(req)
	if err != nil {
		return nil, fmt.Errorf("descargando %s del destino remoto: %w", relPath, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, remoteErr("la descarga de "+relPath, resp)
	}
	return resp.Body, nil
}

func (d *remoteDestination) RemoveFile(ctx context.Context, jobID, relPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, d.fileURL(jobID, relPath), nil)
	if err != nil {
		return err
	}
	resp, err := d.do(req)
	if err != nil {
		return fmt.Errorf("borrando %s en el destino remoto: %w", relPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return remoteErr("el borrado de "+relPath, resp)
	}
	return nil
}

func (d *remoteDestination) RemoveJob(ctx context.Context, jobID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/api/v1/backups/inbound/%s", d.baseURL, jobID), nil)
	if err != nil {
		return err
	}
	resp, err := d.do(req)
	if err != nil {
		return fmt.Errorf("borrando el job %s en el destino remoto: %w", jobID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return remoteErr("el borrado del job "+jobID, resp)
	}
	return nil
}

// Link (ADR-029): un destino remoto nunca puede compartir un inodo con
// otro job -- son dos backups en el mismo servidor, pero un hardlink no
// tiene sentido a través de una API HTTP. Siempre ErrLinkUnsupported;
// Run ya sabe caer a copia completa (ADR-026).
func (d *remoteDestination) Link(ctx context.Context, prevJobID, jobID, relPath string) error {
	return ErrLinkUnsupported
}

// finalize (ADR-029) avisa al receptor de que TODOS los ficheros + el
// manifest.json de jobID ya se subieron con éxito -- solo entonces el
// receptor crea su propia fila en backup_jobs (ver ADR-029 sobre por qué
// hace falta este paso explícito en vez de una fila por PUT). No forma
// parte de la interfaz Destination porque localDestination no necesita
// ningún paso equivalente (writeManifest ya es la señal de éxito local).
func (d *remoteDestination) finalize(ctx context.Context, jobID string) error {
	url := fmt.Sprintf("%s/api/v1/backups/inbound/%s/complete", d.baseURL, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.do(req)
	if err != nil {
		return fmt.Errorf("finalizando el job %s en el destino remoto: %w", jobID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return remoteErr("la finalización del job "+jobID, resp)
	}
	return nil
}
