package storage

import (
	"context"
	"errors"
	"fmt"
)

// ErrOwnerStatusUnavailable se devuelve al resolver un enlace público o de
// subida anónima en un FileService construido sin WithOwnerStatus (ADR-043
// Decisión 4): sin poder comprobar que el propietario sigue activo no se
// sirve nada. Es un fallo de configuración del servidor, no del cliente: los
// handlers lo tratan como error interno.
var ErrOwnerStatusUnavailable = errors.New("storage: la comprobación del estado del propietario no está configurada")

// OwnerStatusChecker dice si el propietario de un recurso publicado sigue
// activo (ADR-043). Lo cumple users.Service.IsActive: storage declara aquí
// lo que necesita para no importar el paquete de usuarios (mismo criterio
// que QuotaResolver).
type OwnerStatusChecker interface {
	IsActive(ctx context.Context, userID string) (bool, error)
}

// WithOwnerStatus conecta la comprobación del estado del propietario que
// exigen los enlaces públicos y los de subida anónima (ADR-043). A
// diferencia de WithQuotas, NO es opcional para esas superficies: sin esta
// opción, su resolución falla con ErrOwnerStatusUnavailable.
func WithOwnerStatus(checker OwnerStatusChecker) FileServiceOption {
	return func(s *FileService) {
		s.ownerStatus = checker
	}
}

// requireActiveOwner devuelve notFound si el propietario no está activo --
// desactivado o inexistente --, para que el enlace sea indistinguible de un
// token que nunca existió y no se filtre el estado de la cuenta (ADR-043
// Decisión 3). Falla cerrado: sin comprobador configurado, o si la consulta
// falla, se devuelve un error y no se sirve nada.
func (s *FileService) requireActiveOwner(ctx context.Context, ownerID string, notFound error) error {
	if s.ownerStatus == nil {
		return ErrOwnerStatusUnavailable
	}
	active, err := s.ownerStatus.IsActive(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("comprobando el estado del propietario: %w", err)
	}
	if !active {
		return notFound
	}
	return nil
}
