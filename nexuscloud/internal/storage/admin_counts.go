package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CountPublicationsForOwner cuenta lo que ownerID tiene publicado y vigente
// (ADR-042 Decisión 8): enlaces públicos, enlaces de subida anónima y
// comparticiones con usuarios o grupos que dan permiso de subida. Pasar una
// cuenta a read_only se rechaza mientras alguno sea distinto de cero, para
// que el cambio de rol no tenga efectos ocultos.
//
// Devuelve enteros sueltos y no un tipo propio para que users.Service pueda
// declarar la interfaz que necesita sin que storage importe users (mismo
// criterio que OwnerStatusChecker y QuotaResolver).
func (s *FileService) CountPublicationsForOwner(ctx context.Context, ownerID string) (publicLinks, anonymousUploads, uploadShares int, err error) {
	now := time.Now().UTC()
	shares, err := s.shares.ListSharesByOwner(ctx, ownerID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("contando comparticiones: %w", err)
	}
	for _, sh := range shares {
		if sh.IsRevoked() || sh.IsExpired(now) {
			continue
		}
		switch {
		case sh.Type == ShareTypeLink:
			publicLinks++
		case sh.CanUpload:
			uploadShares++
		}
	}

	links, err := s.ListAnonymousUploadLinks(ctx, ownerID)
	if err != nil && !errors.Is(err, ErrAnonymousUploadDisabled) {
		return 0, 0, 0, fmt.Errorf("contando enlaces de subida anónima: %w", err)
	}
	for _, a := range links {
		if !a.IsRevoked() && !a.IsExpired(now) {
			anonymousUploads++
		}
	}
	return publicLinks, anonymousUploads, uploadShares, nil
}

// CountActiveSharesForGroup cuenta las comparticiones no revocadas dirigidas
// a groupID: las que se perderán por la cascada de la FK al borrar el grupo
// (ADR-042 Decisión 11).
func (s *FileService) CountActiveSharesForGroup(ctx context.Context, groupID string) (int, error) {
	return s.shares.CountActiveSharesForGroup(ctx, groupID)
}
