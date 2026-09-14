package apiv1

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type rangeResult int

const (
	// rangeNone: no había cabecera Range, o venía en una forma que este
	// servidor no interpreta (con fin explícito, multi-range, unidad
	// distinta de "bytes") -- servir el archivo completo es una respuesta
	// válida en ambos casos (RFC 7233 §3.1 permite ignorar un Range que no
	// se entiende).
	rangeNone rangeResult = iota
	// rangeValid: "bytes=N-" con 0 <= N < size.
	rangeValid
	// rangeUnsatisfiable: "bytes=N-" con N fuera de [0, size) -- el
	// cliente pide reanudar desde un punto que ya no existe (el archivo
	// remoto encogió o cambió de contenido).
	rangeUnsatisfiable
)

// parseResumeRange interpreta la cabecera Range SOLO en la forma
// "bytes=N-" (desde N hasta el final) -- el único caso que el cliente de
// este proyecto necesita para reanudar una descarga cortada (ADR-010,
// §41). Cualquier otra forma (con fin explícito "bytes=N-M", multi-range
// "bytes=N-M,X-Y", o una unidad distinta de "bytes") se trata como
// ausencia de Range: implementar el resto de RFC 7233 no aporta nada
// aquí -- el único cliente de esta API es el propio Flutter, que nunca
// pedirá esas formas.
func parseResumeRange(header string, size int64) (start int64, result rangeResult) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, rangeNone
	}
	spec := strings.TrimPrefix(header, prefix)
	if strings.Contains(spec, ",") || !strings.HasSuffix(spec, "-") {
		return 0, rangeNone
	}
	numPart := strings.TrimSuffix(spec, "-")
	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil || n < 0 {
		// Un número negativo ("bytes=-1-") no es la forma "bytes=N-" que
		// esta función reconoce -- RFC 7233 le da otro significado
		// ("últimos N bytes") que este servidor no soporta. Se trata como
		// formato no reconocido, nunca como rango insatisfacible (eso se
		// reserva para un N sintácticamente válido que excede el tamaño
		// real del archivo).
		return 0, rangeNone
	}
	if n >= size {
		return 0, rangeUnsatisfiable
	}
	return n, rangeValid
}

// serveFileContent escribe las cabeceras y el cuerpo de una respuesta de
// descarga, con soporte de reanudación vía Range (ADR-010, §41). Cierra
// [rc] siempre antes de devolver. Comparte las cinco cabeceras que ya
// escribían por separado DownloadFile/DownloadFileVersion/
// DownloadPublicShare -- [name]/[mimeType]/[sha256Hex]/[sizeBytes] son
// los valores primitivos que cada uno ya extraía de su propio tipo de
// metadata (FileMeta o FileVersion), para que esta función no se acople
// a ninguno de los dos structs concretos.
//
// Si [rc] no implementa io.Seeker (ningún provider hoy lo incumple --
// LocalFilesystemProvider.Read abre con os.Open, y *os.File es Seekable
// -- pero la interfaz storage.Provider deja sitio a uno futuro que no lo
// sea, §157), cualquier Range se ignora y se sirve el archivo completo:
// nunca falla una descarga por esto.
//
// El error que devuelve es el de io.Copy (transmisión ya en curso, las
// cabeceras ya se mandaron) -- quien llama decide cómo loguearlo, con su
// propio contexto (file_id, versión, etc.), igual que ya hacía cada
// handler por separado antes de este slice.
func serveFileContent(
	w http.ResponseWriter,
	r *http.Request,
	name, mimeType, sha256Hex string,
	sizeBytes int64,
	rc io.ReadCloser,
) error {
	defer rc.Close()

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("X-Content-SHA256", sha256Hex)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	start, result := int64(0), rangeNone
	seeker, seekable := rc.(io.Seeker)
	if seekable {
		start, result = parseResumeRange(r.Header.Get("Range"), sizeBytes)
		if result == rangeValid {
			if _, err := seeker.Seek(start, io.SeekStart); err != nil {
				// No debería pasar en la práctica (el archivo ya se abrió
				// bien) -- si pasa, degrada a servir completo en vez de
				// fallar una descarga que habría funcionado sin Range.
				start, result = 0, rangeNone
			}
		}
	}

	switch result {
	case rangeUnsatisfiable:
		// Con el sobre JSON estándar de error (writeError), no solo el
		// status -- así el cliente puede distinguir "416 concreto" de
		// cualquier otro fallo por su `code`, igual que con el resto de la
		// API, en vez de tener que fiarse del status HTTP crudo.
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", sizeBytes))
		writeError(w, http.StatusRequestedRangeNotSatisfiable, "range_not_satisfiable",
			"El rango solicitado no es válido para el tamaño actual del archivo.")
		return nil
	case rangeValid:
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, sizeBytes-1, sizeBytes))
		w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes-start, 10))
		w.WriteHeader(http.StatusPartialContent)
	default: // rangeNone
		w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
		w.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(w, rc)
	return err
}
