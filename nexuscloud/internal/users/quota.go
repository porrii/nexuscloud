package users

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidQuota se devuelve al fijar una cuota negativa. Los valores
// válidos son: nil (hereda), 0 (ilimitada) o un número positivo de bytes.
var ErrInvalidQuota = errors.New("users: la cuota no puede ser negativa (0 = sin límite)")

// QuotaSource indica de dónde sale el límite efectivo de un usuario.
type QuotaSource string

const (
	QuotaSourceNone   QuotaSource = "none"   // sin límite en ningún nivel
	QuotaSourceUser   QuotaSource = "user"   // cuota propia del usuario
	QuotaSourceGroup  QuotaSource = "group"  // cuota de uno de sus grupos
	QuotaSourceGlobal QuotaSource = "global" // storage.defaultQuotaBytes
)

// EffectiveQuota es el resultado de resolver la cuota de un usuario (§24,
// ADR-036). LimitBytes == 0 significa sin límite.
type EffectiveQuota struct {
	LimitBytes int64
	Source     QuotaSource
	// GroupName es el grupo que da el límite cuando Source == QuotaSourceGroup.
	GroupName string
}

func (q EffectiveQuota) Unlimited() bool { return q.LimitBytes == 0 }

// ValidateQuota comprueba una cuota antes de guardarla: nil (hereda), 0
// (ilimitada explícita) o positiva. Lo usan también la CLI y la API antes de
// crear usuarios o grupos.
func ValidateQuota(q *int64) error {
	if q != nil && *q < 0 {
		return ErrInvalidQuota
	}
	return nil
}

// ResolveQuota aplica la jerarquía de §24: la cuota propia del usuario; si no
// tiene, la de sus grupos; si no, la global. En cada nivel nil significa «no
// configurada» y 0 «ilimitada»: por eso un usuario puede eximirse de la cuota
// de su grupo con 0. Un grupo es un límite POR MIEMBRO (no una bolsa
// compartida); con varios grupos gana el más generoso -- 0 (ilimitado) gana a
// cualquier número, y ante un empate se queda el primero (la lista llega
// ordenada por nombre).
func ResolveQuota(userQuota *int64, groups []Group, globalDefault int64) EffectiveQuota {
	if userQuota != nil {
		return EffectiveQuota{LimitBytes: *userQuota, Source: QuotaSourceUser}
	}

	var best *Group
	for i := range groups {
		q := groups[i].QuotaBytes
		if q == nil {
			continue
		}
		if *q == 0 {
			return EffectiveQuota{LimitBytes: 0, Source: QuotaSourceGroup, GroupName: groups[i].Name}
		}
		if best == nil || *q > *best.QuotaBytes {
			best = &groups[i]
		}
	}
	if best != nil {
		return EffectiveQuota{LimitBytes: *best.QuotaBytes, Source: QuotaSourceGroup, GroupName: best.Name}
	}

	if globalDefault > 0 {
		return EffectiveQuota{LimitBytes: globalDefault, Source: QuotaSourceGlobal}
	}
	return EffectiveQuota{Source: QuotaSourceNone}
}

// ServiceOption configura opcionalmente un Service (las llamadas a NewService
// sin opciones siguen compilando igual).
type ServiceOption func(*Service)

// WithDefaultQuota fija la cuota global (storage.defaultQuotaBytes): la que
// se aplica a quien no tiene cuota propia ni de grupo. 0 (o negativa) = sin
// cuota global.
func WithDefaultQuota(bytes int64) ServiceOption {
	return func(s *Service) {
		if bytes > 0 {
			s.defaultQuotaBytes = bytes
		}
	}
}

// EffectiveQuota resuelve el límite de un usuario.
func (s *Service) EffectiveQuota(ctx context.Context, userID string) (EffectiveQuota, error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return EffectiveQuota{}, err
	}
	groups, err := s.repo.GroupsForUser(ctx, userID)
	if err != nil {
		return EffectiveQuota{}, err
	}
	return ResolveQuota(u.QuotaBytes, groups, s.defaultQuotaBytes), nil
}

// LimitFor devuelve solo el número (0 = sin límite): es lo que necesita
// storage.FileService, que declara su propia interfaz QuotaResolver para no
// importar este paquete.
func (s *Service) LimitFor(ctx context.Context, userID string) (int64, error) {
	q, err := s.EffectiveQuota(ctx, userID)
	if err != nil {
		return 0, err
	}
	return q.LimitBytes, nil
}

// SetUserQuota fija la cuota propia de un usuario: nil = hereda (grupo o
// global), 0 = ilimitada, >0 = límite en bytes.
func (s *Service) SetUserQuota(ctx context.Context, userID string, quota *int64) error {
	if err := ValidateQuota(quota); err != nil {
		return err
	}
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	u.QuotaBytes = copyQuota(quota)
	u.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateUser(ctx, u)
}

// SetGroupQuota fija la cuota de un grupo (por miembro): nil = el grupo no
// aporta cuota, 0 = ilimitada para sus miembros, >0 = límite en bytes.
func (s *Service) SetGroupQuota(ctx context.Context, groupID string, quota *int64) error {
	if err := ValidateQuota(quota); err != nil {
		return err
	}
	return s.repo.UpdateGroupQuota(ctx, groupID, copyQuota(quota))
}

func copyQuota(q *int64) *int64 {
	if q == nil {
		return nil
	}
	v := *q
	return &v
}
