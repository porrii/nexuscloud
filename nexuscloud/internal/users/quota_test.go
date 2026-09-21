package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

func i64(v int64) *int64 { return &v }

// ResolveQuota (§24, ADR-036): cuota propia -> la del grupo -> la global.
// nil = hereda, 0 = ilimitado explícito, >0 = límite en bytes.
func TestResolveQuota(t *testing.T) {
	grp := func(name string, q *int64) Group { return Group{ID: name, Name: name, QuotaBytes: q} }
	tests := []struct {
		name   string
		user   *int64
		groups []Group
		global int64
		want   EffectiveQuota
	}{
		{"sin nada configurado: ilimitada", nil, nil, 0,
			EffectiveQuota{LimitBytes: 0, Source: QuotaSourceNone}},
		{"solo la global", nil, nil, 300,
			EffectiveQuota{LimitBytes: 300, Source: QuotaSourceGlobal}},
		{"la propia gana a grupo y global", i64(100), []Group{grp("a", i64(500))}, 300,
			EffectiveQuota{LimitBytes: 100, Source: QuotaSourceUser}},
		{"propia 0 = ilimitada explícita, gana a grupo y global", i64(0), []Group{grp("a", i64(500))}, 300,
			EffectiveQuota{LimitBytes: 0, Source: QuotaSourceUser}},
		{"un grupo con cuota gana a la global", nil, []Group{grp("a", i64(500))}, 300,
			EffectiveQuota{LimitBytes: 500, Source: QuotaSourceGroup, GroupName: "a"}},
		{"un grupo con cuota menor que la global también gana", nil, []Group{grp("a", i64(100))}, 300,
			EffectiveQuota{LimitBytes: 100, Source: QuotaSourceGroup, GroupName: "a"}},
		{"grupos sin cuota no cuentan", nil, []Group{grp("a", nil), grp("b", nil)}, 300,
			EffectiveQuota{LimitBytes: 300, Source: QuotaSourceGlobal}},
		{"varios grupos: gana el más generoso", nil,
			[]Group{grp("a", i64(200)), grp("b", i64(700)), grp("c", i64(400))}, 0,
			EffectiveQuota{LimitBytes: 700, Source: QuotaSourceGroup, GroupName: "b"}},
		{"un grupo ilimitado (0) gana a todos los demás", nil,
			[]Group{grp("a", i64(700)), grp("b", i64(0))}, 300,
			EffectiveQuota{LimitBytes: 0, Source: QuotaSourceGroup, GroupName: "b"}},
		{"empate entre grupos: el primero", nil,
			[]Group{grp("a", i64(700)), grp("b", i64(700))}, 0,
			EffectiveQuota{LimitBytes: 700, Source: QuotaSourceGroup, GroupName: "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveQuota(tc.user, tc.groups, tc.global)
			if got != tc.want {
				t.Errorf("ResolveQuota = %+v, esperado %+v", got, tc.want)
			}
		})
	}
}

func TestEffectiveQuotaUnlimited(t *testing.T) {
	if !(EffectiveQuota{LimitBytes: 0}).Unlimited() {
		t.Error("LimitBytes 0 debería ser ilimitada")
	}
	if (EffectiveQuota{LimitBytes: 1}).Unlimited() {
		t.Error("LimitBytes 1 no debería ser ilimitada")
	}
}

func TestValidateQuota(t *testing.T) {
	if err := ValidateQuota(nil); err != nil {
		t.Errorf("nil (hereda) debe ser válida: %v", err)
	}
	if err := ValidateQuota(i64(0)); err != nil {
		t.Errorf("0 (ilimitada) debe ser válida: %v", err)
	}
	if err := ValidateQuota(i64(5 << 30)); err != nil {
		t.Errorf("5 GiB debe ser válida: %v", err)
	}
	if err := ValidateQuota(i64(-1)); !errors.Is(err, ErrInvalidQuota) {
		t.Errorf("-1 = %v, esperado ErrInvalidQuota", err)
	}
}

func TestEffectiveQuotaResolvesUserThenGroupThenGlobal(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo, WithDefaultQuota(300))

	u, err := svc.CreateUser(ctx, CreateUserInput{Username: "ana", PasswordHash: "h"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := svc.EffectiveQuota(ctx, u.ID)
	if err != nil {
		t.Fatalf("EffectiveQuota: %v", err)
	}
	if got.LimitBytes != 300 || got.Source != QuotaSourceGlobal {
		t.Errorf("sin cuota propia ni de grupo = %+v, esperado la global (300)", got)
	}

	grp := &Group{ID: idgen.New(), Name: "familia", QuotaBytes: i64(500), CreatedAt: time.Now().UTC()}
	if err := repo.CreateGroup(ctx, grp); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := repo.AddUserToGroup(ctx, u.ID, grp.ID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	got, _ = svc.EffectiveQuota(ctx, u.ID)
	if got.LimitBytes != 500 || got.Source != QuotaSourceGroup || got.GroupName != "familia" {
		t.Errorf("con grupo = %+v, esperado 500 del grupo familia", got)
	}

	if err := svc.SetUserQuota(ctx, u.ID, i64(100)); err != nil {
		t.Fatalf("SetUserQuota: %v", err)
	}
	got, _ = svc.EffectiveQuota(ctx, u.ID)
	if got.LimitBytes != 100 || got.Source != QuotaSourceUser {
		t.Errorf("con cuota propia = %+v, esperado 100 del usuario", got)
	}

	if err := svc.SetUserQuota(ctx, u.ID, nil); err != nil {
		t.Fatalf("SetUserQuota(nil): %v", err)
	}
	got, _ = svc.EffectiveQuota(ctx, u.ID)
	if got.LimitBytes != 500 || got.Source != QuotaSourceGroup {
		t.Errorf("tras volver a heredar = %+v, esperado otra vez el grupo", got)
	}
}

// LimitFor es lo que consume storage.FileService (interfaz QuotaResolver):
// solo el número, 0 = sin límite.
func TestLimitFor(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t), WithDefaultQuota(300))
	u, _ := svc.CreateUser(ctx, CreateUserInput{Username: "bea", PasswordHash: "h"})

	if got, err := svc.LimitFor(ctx, u.ID); err != nil || got != 300 {
		t.Errorf("LimitFor = (%d, %v), esperado (300, nil)", got, err)
	}
	if err := svc.SetUserQuota(ctx, u.ID, i64(0)); err != nil {
		t.Fatalf("SetUserQuota(0): %v", err)
	}
	if got, err := svc.LimitFor(ctx, u.ID); err != nil || got != 0 {
		t.Errorf("con ilimitada explícita LimitFor = (%d, %v), esperado (0, nil)", got, err)
	}
	if _, err := svc.LimitFor(ctx, "no-existe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("usuario inexistente = %v, esperado ErrNotFound", err)
	}
}

func TestWithDefaultQuotaIgnoresNegative(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t), WithDefaultQuota(-5))
	u, _ := svc.CreateUser(ctx, CreateUserInput{Username: "cai", PasswordHash: "h"})
	if got, _ := svc.LimitFor(ctx, u.ID); got != 0 {
		t.Errorf("una global negativa debe tratarse como sin límite, LimitFor = %d", got)
	}
}

func TestSetUserQuotaValidates(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t))
	u, _ := svc.CreateUser(ctx, CreateUserInput{Username: "dan", PasswordHash: "h"})

	if err := svc.SetUserQuota(ctx, u.ID, i64(-1)); !errors.Is(err, ErrInvalidQuota) {
		t.Errorf("cuota negativa = %v, esperado ErrInvalidQuota", err)
	}
	if err := svc.SetUserQuota(ctx, "no-existe", i64(10)); !errors.Is(err, ErrNotFound) {
		t.Errorf("usuario inexistente = %v, esperado ErrNotFound", err)
	}
	if err := svc.SetUserQuota(ctx, u.ID, i64(5<<30)); err != nil {
		t.Fatalf("5 GiB: %v", err)
	}
	stored, _ := svc.repo.GetUserByID(ctx, u.ID)
	if stored.QuotaBytes == nil || *stored.QuotaBytes != 5<<30 {
		t.Errorf("cuota guardada = %v, esperado 5 GiB", stored.QuotaBytes)
	}
}

func TestCreateUserQuota(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newTestRepo(t))

	u, err := svc.CreateUser(ctx, CreateUserInput{Username: "eva", PasswordHash: "h", QuotaBytes: i64(7 << 30)})
	if err != nil {
		t.Fatalf("CreateUser con cuota: %v", err)
	}
	if u.QuotaBytes == nil || *u.QuotaBytes != 7<<30 {
		t.Errorf("cuota = %v, esperado 7 GiB", u.QuotaBytes)
	}
	if _, err := svc.CreateUser(ctx, CreateUserInput{Username: "fer", PasswordHash: "h", QuotaBytes: i64(-1)}); !errors.Is(err, ErrInvalidQuota) {
		t.Errorf("cuota negativa al crear = %v, esperado ErrInvalidQuota", err)
	}
	if _, err := svc.repo.GetUserByUsername(ctx, "fer"); !errors.Is(err, ErrNotFound) {
		t.Errorf("un alta rechazada no debe dejar el usuario creado: %v", err)
	}
}

func TestSetGroupQuota(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	svc := NewService(repo)
	grp := &Group{ID: idgen.New(), Name: "trabajo", CreatedAt: time.Now().UTC()}
	if err := repo.CreateGroup(ctx, grp); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	if err := svc.SetGroupQuota(ctx, grp.ID, i64(1<<40)); err != nil {
		t.Fatalf("SetGroupQuota: %v", err)
	}
	stored, _ := repo.GetGroupByID(ctx, grp.ID)
	if stored.QuotaBytes == nil || *stored.QuotaBytes != 1<<40 {
		t.Errorf("cuota del grupo = %v, esperado 1 TiB", stored.QuotaBytes)
	}
	// Repetir el mismo valor no es un error (en MySQL informa 0 filas
	// cambiadas: no debe confundirse con «grupo inexistente»).
	if err := svc.SetGroupQuota(ctx, grp.ID, i64(1<<40)); err != nil {
		t.Errorf("mismo valor dos veces = %v, esperado nil", err)
	}
	if err := svc.SetGroupQuota(ctx, grp.ID, nil); err != nil {
		t.Fatalf("SetGroupQuota(nil): %v", err)
	}
	stored, _ = repo.GetGroupByID(ctx, grp.ID)
	if stored.QuotaBytes != nil {
		t.Errorf("tras quitar la cuota = %v, esperado nil", *stored.QuotaBytes)
	}
	if err := svc.SetGroupQuota(ctx, grp.ID, i64(-1)); !errors.Is(err, ErrInvalidQuota) {
		t.Errorf("cuota negativa = %v, esperado ErrInvalidQuota", err)
	}
	if err := svc.SetGroupQuota(ctx, "no-existe", i64(10)); !errors.Is(err, ErrNotFound) {
		t.Errorf("grupo inexistente = %v, esperado ErrNotFound", err)
	}
}
