//go:build !linux && !windows

package diskinfo

import "context"

// enumerate en plataformas sin adaptador (p.ej. macOS, *BSD): el requisito
// 14 exige que el núcleo no dependa de una plataforma concreta, así que
// aquí se devuelve ErrUnsupported en vez de romper la compilación. Añadir
// un adaptador nuevo es solo crear otro fichero enumerate_<os>.go.
func enumerate(_ context.Context) ([]Disk, error) {
	return nil, ErrUnsupported
}
