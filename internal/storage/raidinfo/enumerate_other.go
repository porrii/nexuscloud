//go:build !linux

package raidinfo

import "context"

// enumerate en plataformas sin adaptador -- incluido Windows: Storage
// Spaces queda para un slice futuro dedicado (mecanismo y verificación
// completamente distintos de mdadm/Linux). Devolver ErrUnsupported en vez
// de romper la compilación o fingir soporte que no existe (§12/§14).
func enumerate(_ context.Context) ([]RaidArray, error) {
	return nil, ErrUnsupported
}
