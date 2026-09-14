package storage

import (
	"errors"
	"os"
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

// SafeJoinResolved amplía SafeJoin con protección frente a symlinks. SafeJoin
// neutraliza "../" y rutas absolutas, pero un symlink creado DENTRO de la
// raíz de un pool y apuntando fuera escaparía igualmente en lectura o
// escritura. Esta variante, además de la garantía léxica de SafeJoin,
// recorre hacia arriba el ancestro existente más profundo del resultado y
// comprueba que su forma canónica (filepath.EvalSymlinks) sigue dentro de
// resolvedRoot; si algún componente del camino es un symlink, lo rechaza.
//
// resolvedRoot DEBE venir ya canónico -- filepath.EvalSymlinks aplicado una
// sola vez al construir el provider (ver LocalFilesystemProvider). Así la
// propia raíz puede ser un symlink legítimo (p.ej. storageDir apuntando a un
// disco montado) sin penalizar cada operación con un EvalSymlinks completo.
func SafeJoinResolved(resolvedRoot, relPath string) (string, error) {
	full, err := SafeJoin(resolvedRoot, relPath)
	if err != nil {
		return "", err
	}

	// El destino puede no existir todavía (Write). Se busca el ancestro
	// existente más profundo; SafeJoin ya garantiza que la cola inexistente
	// entre ese ancestro y `full` no contiene "..", así que no puede escapar
	// por sí sola.
	probe := full
	for {
		if probe == resolvedRoot {
			// resolvedRoot ya es canónico por construcción.
			return full, nil
		}

		fi, lerr := os.Lstat(probe)
		if lerr != nil {
			if !os.IsNotExist(lerr) {
				return "", lerr
			}
			parent := filepath.Dir(probe)
			if parent == probe {
				// Raíz del volumen sin encontrar nada existente: imposible si
				// SafeJoin hizo su trabajo, pero se rechaza por si acaso.
				return "", ErrPathEscapesRoot
			}
			probe = parent
			continue
		}

		// `probe` existe. Si es un symlink (roto o no), no se puede
		// garantizar que su destino quede dentro: se rechaza.
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", ErrPathEscapesRoot
		}

		resolved, rerr := filepath.EvalSymlinks(probe)
		if rerr != nil {
			return "", rerr
		}
		if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
			return "", ErrPathEscapesRoot
		}
		return full, nil
	}
}

// ResolveRoot deja una ruta de raíz de almacenamiento en su forma canónica
// (absoluta, con los symlinks resueltos) creándola si no existe. Es lo que
// deben usar los constructores de provider/layout para fijar la raíz una
// sola vez; a partir de ahí SafeJoinResolved solo resuelve el lado del
// destino.
func ResolveRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}
