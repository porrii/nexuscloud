//go:build !linux && !windows

package raidinfo

import "context"

// enumerate en plataformas sin adaptador (p.ej. macOS, *BSD) -- Linux
// (mdadm) y Windows (Storage Spaces) ya tienen el suyo propio. Devolver
// ErrUnsupported en vez de romper la compilación o fingir soporte que no
// existe (§12/§14).
func enumerate(_ context.Context) ([]RaidArray, error) {
	return nil, ErrUnsupported
}
