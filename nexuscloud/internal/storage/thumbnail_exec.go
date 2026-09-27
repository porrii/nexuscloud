package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// ErrThumbnailBinaryNotFound: ffmpeg/poppler-utils no están instalados en
// este servidor -- degrada con gracia (§34 Decisión 4), nunca panic ni
// cuelgue. Cada intento fallido queda en thumbnail_jobs.last_error con
// este mensaje; tras agotar los reintentos se audita una vez
// (EventThumbnailGenerationFailed) -- no hace falta un mecanismo de log
// aparte "una sola vez": el propio ciclo de vida del job ya da esa
// visibilidad, por archivo, sin duplicar bookkeeping.
var ErrThumbnailBinaryNotFound = errors.New("storage: el binario externo necesario no está instalado en este servidor")

// ExecThumbnailLimits agrupa los controles de seguridad de los pipelines
// de vídeo/PDF (§34, ADR-041 Decisión 4) -- el primer exec.Command del
// proyecto sobre contenido no confiable.
type ExecThumbnailLimits struct {
	MaxInputBytes int64
	Timeout       time.Duration
}

// GenerateVideoThumbnail extrae el frame de 1s de un vídeo con ffmpeg y
// lo devuelve como JPEG ya redimensionado a 320px de ancho. sizeBytes es
// el tamaño YA CONOCIDO del archivo -- se comprueba contra
// limits.MaxInputBytes ANTES de invocar ffmpeg. inputPath es la ruta
// física interna del propio storage (nunca una cadena que el cliente
// controle); tempDir es un directorio de escritura propio del servidor
// (Layout.Temp) para el fichero de salida intermedio, borrado siempre al
// terminar.
//
// -protocol_whitelist file (hallazgo ALTO del pase de security-reviewer
// sobre el diseño, §34 Decisión 4): "args como slice" evita que el
// ATACANTE inyecte comandos vía nombre de archivo/metadata, pero NO evita
// que el propio CONTENIDO del archivo le pida a ffmpeg abrir otra cosa --
// varios demuxers (HLS/m3u8, concat, subfile) siguen referencias a
// rutas/protocolos EMBEBIDAS DENTRO del archivo. Sin esto, un .mp4 cuyo
// contenido real es una playlist podría hacer que ffmpeg abra
// file:///etc/passwd (LFI) o una URL interna (SSRF), corriendo como el
// usuario del servicio -- que ya tiene acceso a todos los pools. -ss antes
// de -i (fast-seek por contenedor) y -frames:v 1 acotan también el peor
// caso de trabajo antes de que el timeout pueda dispararse.
func GenerateVideoThumbnail(ctx context.Context, inputPath, tempDir string, sizeBytes int64, limits ExecThumbnailLimits) ([]byte, error) {
	if sizeBytes > limits.MaxInputBytes {
		return nil, ErrThumbnailInputTooLarge
	}
	if _, err := execLookPath("ffmpeg"); err != nil {
		return nil, ErrThumbnailBinaryNotFound
	}

	outputPath := filepath.Join(tempDir, "thumb-"+idgen.New()+".jpg")
	defer os.Remove(outputPath)

	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-protocol_whitelist", "file",
		"-ss", "00:00:01",
		"-i", inputPath,
		"-frames:v", "1",
		"-vf", "scale=320:-1",
		"-y", outputPath,
	)
	configureSysProcAttr(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ErrThumbnailTimeout
		}
		return nil, fmt.Errorf("ffmpeg falló: %w (%s)", err, firstLine(stderr.String()))
	}
	return os.ReadFile(outputPath)
}

// GeneratePDFThumbnail rasteriza la primera página de un PDF con
// pdftoppm (poppler-utils) y la devuelve como JPEG ya redimensionado.
// Mismos criterios que GenerateVideoThumbnail (args como slice, ruta
// interna, límite de tamaño de entrada, timeout con SIGKILL al grupo de
// procesos en Linux).
//
// -f 1 -l 1 (hallazgo BAJO del pase de seguridad): sin este límite
// explícito, un PDF diminuto en bytes con miles de páginas en blanco
// podría hacer que pdftoppm intentara escribir miles de archivos antes de
// que el timeout lo matara. -singlefile fuerza un nombre de salida
// predecible (<prefijo>.jpg, sin el sufijo "-1" que pdftoppm añadiría por
// defecto), evitando adivinar el nombre exacto que puso.
func GeneratePDFThumbnail(ctx context.Context, inputPath, tempDir string, sizeBytes int64, limits ExecThumbnailLimits) ([]byte, error) {
	if sizeBytes > limits.MaxInputBytes {
		return nil, ErrThumbnailInputTooLarge
	}
	if _, err := execLookPath("pdftoppm"); err != nil {
		return nil, ErrThumbnailBinaryNotFound
	}

	outputPrefix := filepath.Join(tempDir, "thumb-"+idgen.New())
	outputPath := outputPrefix + ".jpg"
	defer os.Remove(outputPath)

	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdftoppm",
		"-f", "1", "-l", "1", "-singlefile",
		"-jpeg", "-scale-to", "320",
		inputPath, outputPrefix,
	)
	configureSysProcAttr(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ErrThumbnailTimeout
		}
		return nil, fmt.Errorf("pdftoppm falló: %w (%s)", err, firstLine(stderr.String()))
	}
	return os.ReadFile(outputPath)
}

// execLookPath es una indirección para poder simular "binario ausente" en
// tests sin depender de qué haya instalado de verdad el entorno que corre
// go test.
var execLookPath = exec.LookPath

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
