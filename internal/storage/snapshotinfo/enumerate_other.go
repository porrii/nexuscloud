//go:build !windows

package snapshotinfo

import "context"

// enumerate en plataformas sin adaptador (p.ej. Linux, macOS, *BSD) --
// Windows (VSS) ya tiene el suyo propio. Devolver ErrUnsupported en vez de
// romper la compilación o fingir soporte que no existe (§14/§17).
func enumerate(_ context.Context) ([]Snapshot, error) {
	return nil, ErrUnsupported
}
