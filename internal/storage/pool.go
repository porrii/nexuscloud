package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrPoolNotFound      = errors.New("storage: storage pool no encontrado")
	ErrPoolInUse         = errors.New("storage: el storage pool tiene archivos o carpetas y no se puede borrar")
	ErrLastActivePool    = errors.New("storage: no se puede desactivar el último storage pool activo")
	ErrUnsupportedPool   = errors.New("storage: tipo de storage pool no soportado")
	ErrInvalidPoolPolicy = errors.New("storage: valor de política de storage pool inválido")
)

// Políticas por pool (Fase D). Se guardan siempre; en la Fase D solo se
// honra utilización "fill" (los ficheros nuevos van al pool activo de
// mayor prioridad). El resto quedan reservadas para fases posteriores.
const (
	UtilizationFill       = "fill"
	UtilizationRoundRobin = "round-robin"
	UtilizationManual     = "manual"

	PolicyInherit = "inherit"
	PolicyOn      = "on"
	PolicyOff     = "off"

	SnapshotNone = "none"
)

// Pool representa un Storage Pool (§10). Fase D soporta solo pools locales
// de tipo "local"; RAID/discos/SMART (§11-12) se detectan del SO, no se
// implementan aquí.
type Pool struct {
	ID        string
	Name      string
	Type      string // "local"
	Path      string
	Priority  int
	Status    string // "active" | "disabled"
	CreatedAt time.Time

	UtilizationPolicy string // fill | round-robin | manual
	BackupPolicy      string // inherit | on | off
	VersioningPolicy  string // inherit | on | off
	SnapshotPolicy    string // none
}

// defaultPolicies rellena las políticas vacías con su valor por defecto
// (el mismo DEFAULT que la migración 0006). Se llama al crear un pool.
func (p *Pool) defaultPolicies() {
	if p.UtilizationPolicy == "" {
		p.UtilizationPolicy = UtilizationFill
	}
	if p.BackupPolicy == "" {
		p.BackupPolicy = PolicyInherit
	}
	if p.VersioningPolicy == "" {
		p.VersioningPolicy = PolicyInherit
	}
	if p.SnapshotPolicy == "" {
		p.SnapshotPolicy = SnapshotNone
	}
}

// validatePolicies comprueba que cada política tiene un valor conocido.
func (p *Pool) validatePolicies() error {
	if !oneOf(p.UtilizationPolicy, UtilizationFill, UtilizationRoundRobin, UtilizationManual) {
		return fmt.Errorf("%w: utilization=%q", ErrInvalidPoolPolicy, p.UtilizationPolicy)
	}
	if !oneOf(p.BackupPolicy, PolicyInherit, PolicyOn, PolicyOff) {
		return fmt.Errorf("%w: backup=%q", ErrInvalidPoolPolicy, p.BackupPolicy)
	}
	if !oneOf(p.VersioningPolicy, PolicyInherit, PolicyOn, PolicyOff) {
		return fmt.Errorf("%w: versioning=%q", ErrInvalidPoolPolicy, p.VersioningPolicy)
	}
	if !oneOf(p.SnapshotPolicy, SnapshotNone) {
		return fmt.Errorf("%w: snapshot=%q", ErrInvalidPoolPolicy, p.SnapshotPolicy)
	}
	return nil
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

type PoolRepository interface {
	CreatePool(ctx context.Context, p *Pool) error
	GetPoolByID(ctx context.Context, id string) (*Pool, error)
	ListPools(ctx context.Context) ([]*Pool, error)
	// DefaultPool devuelve el pool activo de mayor prioridad. No crea
	// ninguno: eso es responsabilidad de EnsureDefaultPool en el arranque
	// del servidor (bootstrap.go), para mantener el repositorio libre de
	// efectos secundarios implícitos.
	DefaultPool(ctx context.Context) (*Pool, error)

	// UpdatePool persiste nombre, prioridad y políticas de un pool ya
	// existente (no toca id/type/path/status/created_at).
	UpdatePool(ctx context.Context, p *Pool) error
	// SetPoolStatus cambia el estado ("active"/"disabled"). El llamador es
	// responsable de no dejar el sistema sin ningún pool activo.
	SetPoolStatus(ctx context.Context, id, status string) error
	// DeletePool borra un pool. Devuelve ErrPoolInUse si hay files o
	// directories que lo referencian (la FK ON DELETE RESTRICT lo impide;
	// esto lo traduce a un error de dominio legible).
	DeletePool(ctx context.Context, id string) error
}
