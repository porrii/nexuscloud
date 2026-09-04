package storage

import (
	"errors"
	"path/filepath"
	"strings"
)

var (
	ErrPathEscapesRoot = errors.New("storage: la ruta intenta escapar de la raíz de almacenamiento")
	ErrInvalidPath     = errors.New("storage: ruta inválida")
)

// SafeJoin une root con relPath garantizando que el resultado permanece
// dentro de root pase lo que pase en relPath ("../", rutas absolutas,
// bytes nulos, etc.). Es el único mecanismo permitido para construir una
// ruta de archivo a partir de entrada no confiable (§73, §194-196):
// LocalFilesystemProvider lo aplica internamente a cada operación, con
// independencia de que la capa superior (FileService) ya haya normalizado
// la ruta lógica — defensa en profundidad (§168 Zero Trust).
func SafeJoin(root, relPath string) (string, error) {
	if strings.ContainsRune(relPath, 0) {
		return "", ErrInvalidPath
	}

	// Anclar relPath a una raíz virtual "/" antes de limpiar: filepath.Clean
	// nunca deja que ".." suba por encima de una ruta absoluta, así que
	// cualquier intento de escape queda neutralizado antes de tocar el
	// filesystem real.
	cleanRel := filepath.Clean(string(filepath.Separator) + filepath.FromSlash(relPath))
	full := filepath.Join(root, cleanRel)

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}

	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(filepath.Separator)) {
		return "", ErrPathEscapesRoot
	}
	return fullAbs, nil
}
