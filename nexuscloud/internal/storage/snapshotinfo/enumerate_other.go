//go:build !windows && !linux

package snapshotinfo

import "context"

// enumerate en plataformas sin adaptador (p.ej. macOS, *BSD) -- Windows
// (VSS) y Linux (ZFS+Btrfs) ya tienen el suyo propio. Devolver
// ErrUnsupported en vez de romper la compilación o fingir soporte que no
// existe (§14/§17).
func enumerate(_ context.Context) ([]Snapshot, error) {
	return nil, ErrUnsupported
}
