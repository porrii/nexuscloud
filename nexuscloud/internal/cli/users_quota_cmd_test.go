package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
)

// Cuotas por CLI (§24, ADR-036): --quota en users create/edit y en el grupo,
// el informe `users quota` y que `files upload` también las respeta.

func ptr64(v int64) *int64 { return &v }

func TestParseQuotaFlag(t *testing.T) {
	const gib = int64(1) << 30
	tests := []struct {
		in      string
		want    *int64
		wantErr bool
	}{
		{"100GB", ptr64(100 * gib), false},
		{"100gb", ptr64(100 * gib), false},
		{"100 GB", ptr64(100 * gib), false},
		{"100GiB", ptr64(100 * gib), false},
		{"1.5TB", ptr64(3 * (int64(1) << 39)), false},
		{"500MB", ptr64(500 << 20), false},
		{"64KB", ptr64(64 << 10), false},
		{"1PB", ptr64(int64(1) << 50), false},
		{"2048", ptr64(2048), false},
		{"512B", ptr64(512), false},
		{"0", ptr64(0), false},
		{"0GB", ptr64(0), false},
		{"unlimited", ptr64(0), false},
		{"Ilimitada", ptr64(0), false},
		{"ilimitado", ptr64(0), false},
		{"inherit", nil, false},
		{"heredar", nil, false},
		{"none", nil, false},
		{"", nil, true},
		{"-5GB", nil, true},
		{"-1", nil, true},
		{"abc", nil, true},
		{"10XB", nil, true},
		{"GB", nil, true},
		{"1.5", nil, true}, // decimales solo con unidad: no hay medio byte
		{"9999999PB", nil, true},
		{"99999999999999999999", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseQuotaFlag(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseQuotaFlag(%q) = %v, esperado un error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseQuotaFlag(%q) falló: %v", tc.in, err)
			}
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("parseQuotaFlag(%q) = %v, esperado %v", tc.in, deref(got), deref(tc.want))
			}
		})
	}
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestFormatQuota(t *testing.T) {
	if got := formatQuota(nil); got != "heredada" {
		t.Errorf("formatQuota(nil) = %q, esperado heredada", got)
	}
	if got := formatQuota(ptr64(0)); got != "ilimitada" {
		t.Errorf("formatQuota(0) = %q, esperado ilimitada", got)
	}
	if got := formatQuota(ptr64(100 << 30)); got != "100.0 GiB" {
		t.Errorf("formatQuota(100 GiB) = %q, esperado 100.0 GiB", got)
	}
}

// storedQuota lee de la base de datos de prueba la cuota propia de un usuario.
func storedQuota(t *testing.T, username string) *int64 {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	u, err := userRepo.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername falló: %v", err)
	}
	return u.QuotaBytes
}

func storedGroupQuota(t *testing.T, group string) *int64 {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load falló: %v", err)
	}
	sqlDB, userRepo, err := openUsersRepo(cfg, false)
	if err != nil {
		t.Fatalf("openUsersRepo falló: %v", err)
	}
	defer sqlDB.Close()
	g, err := userRepo.GetGroupByName(context.Background(), group)
	if err != nil {
		t.Fatalf("GetGroupByName falló: %v", err)
	}
	return g.QuotaBytes
}

func TestUsersCreateWithQuota(t *testing.T) {
	newCLITestUser(t) // solo prepara la base de datos de prueba

	if out, err := runUsers(t, "create", "--username", "conlimite", "--password", "contrasena-larga-1", "--quota", "5GB"); err != nil {
		t.Fatalf("create --quota falló: %v (salida: %s)", err, out)
	}
	if q := storedQuota(t, "conlimite"); q == nil || *q != 5<<30 {
		t.Errorf("cuota guardada = %v, esperado 5 GiB", deref(q))
	}

	if _, err := runUsers(t, "create", "--username", "malo", "--password", "contrasena-larga-1", "--quota", "muchos"); err == nil {
		t.Error("una cuota inválida debe rechazarse")
	}
	cfg, _ := config.Load("")
	sqlDB, userRepo, _ := openUsersRepo(cfg, false)
	defer sqlDB.Close()
	if _, err := userRepo.GetUserByUsername(context.Background(), "malo"); err == nil {
		t.Error("un alta con cuota inválida no debe dejar el usuario creado")
	}
}

func TestUsersEditQuota(t *testing.T) {
	username := newCLITestUser(t)

	if out, err := runUsers(t, "edit", username, "--quota", "100MB"); err != nil {
		t.Fatalf("edit --quota falló: %v (salida: %s)", err, out)
	}
	if q := storedQuota(t, username); q == nil || *q != 100<<20 {
		t.Fatalf("cuota = %v, esperado 100 MiB", deref(q))
	}
	if _, err := runUsers(t, "edit", username, "--quota", "unlimited"); err != nil {
		t.Fatalf("edit --quota unlimited falló: %v", err)
	}
	if q := storedQuota(t, username); q == nil || *q != 0 {
		t.Errorf("cuota = %v, esperado 0 (ilimitada)", deref(q))
	}
	if _, err := runUsers(t, "edit", username, "--quota", "inherit"); err != nil {
		t.Fatalf("edit --quota inherit falló: %v", err)
	}
	if q := storedQuota(t, username); q != nil {
		t.Errorf("cuota = %v, esperado nil (hereda)", *q)
	}

	// Una cuota inválida no cambia nada.
	runUsers(t, "edit", username, "--quota", "3GB")
	if _, err := runUsers(t, "edit", username, "--quota", "-3GB"); err == nil {
		t.Error("una cuota negativa debe rechazarse")
	}
	if q := storedQuota(t, username); q == nil || *q != 3<<30 {
		t.Errorf("cuota = %v, esperado que se conserven los 3 GiB tras el intento inválido", deref(q))
	}

	// --quota cuenta como «algo que editar» y se combina con los demás flags.
	if _, err := runUsers(t, "edit", username, "--quota", "1GB", "--display-name", "Con Cuota"); err != nil {
		t.Fatalf("edit combinado falló: %v", err)
	}
	cfg, _ := config.Load("")
	sqlDB, userRepo, _ := openUsersRepo(cfg, false)
	defer sqlDB.Close()
	u, _ := userRepo.GetUserByUsername(context.Background(), username)
	if u.DisplayName != "Con Cuota" || u.QuotaBytes == nil || *u.QuotaBytes != 1<<30 {
		t.Errorf("tras el edit combinado: nombre=%q cuota=%v", u.DisplayName, deref(u.QuotaBytes))
	}
}

func TestUsersGroupCreateWithQuotaAndEdit(t *testing.T) {
	newCLITestUser(t)

	if out, err := runUsers(t, "group", "create", "Familia", "--quota", "500GB"); err != nil {
		t.Fatalf("group create --quota falló: %v (salida: %s)", err, out)
	}
	if q := storedGroupQuota(t, "Familia"); q == nil || *q != 500<<30 {
		t.Fatalf("cuota del grupo = %v, esperado 500 GiB", deref(q))
	}

	if out, err := runUsers(t, "group", "edit", "Familia", "--quota", "unlimited"); err != nil {
		t.Fatalf("group edit falló: %v (salida: %s)", err, out)
	}
	if q := storedGroupQuota(t, "Familia"); q == nil || *q != 0 {
		t.Errorf("cuota del grupo = %v, esperado 0 (ilimitada)", deref(q))
	}
	if _, err := runUsers(t, "group", "edit", "Familia", "--quota", "inherit"); err != nil {
		t.Fatalf("group edit inherit falló: %v", err)
	}
	if q := storedGroupQuota(t, "Familia"); q != nil {
		t.Errorf("cuota del grupo = %v, esperado nil", *q)
	}

	if _, err := runUsers(t, "group", "edit", "NoExiste", "--quota", "1GB"); err == nil {
		t.Error("editar un grupo inexistente debe fallar")
	}
	if _, err := runUsers(t, "group", "edit", "Familia"); err == nil || !strings.Contains(err.Error(), "--quota") {
		t.Errorf("group edit sin --quota = %v, esperado un error que pida --quota", err)
	}
	if _, err := runUsers(t, "group", "create", "Mala", "--quota", "-1"); err == nil {
		t.Error("crear un grupo con cuota negativa debe fallar")
	}
}

// writeLocalFile crea un fichero local de n bytes y devuelve su ruta.
func writeLocalFile(t *testing.T, name string, n int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(strings.Repeat("x", n)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFilesUploadRespectsTheQuota(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runUsers(t, "edit", username, "--quota", "50"); err != nil {
		t.Fatalf("fijando la cuota: %v", err)
	}

	_, err := runFiles(t, "upload", "--username", username, writeLocalFile(t, "grande.bin", 60), "/grande.bin")
	if err == nil || !strings.Contains(err.Error(), "cuota") {
		t.Fatalf("subir 60 bytes con cuota de 50 = %v, esperado un error de cuota", err)
	}
	if out, err := runFiles(t, "upload", "--username", username, writeLocalFile(t, "cabe.bin", 30), "/cabe.bin"); err != nil {
		t.Errorf("subir 30 bytes con cuota de 50 falló: %v (salida: %s)", err, out)
	}
}

func TestUsersQuotaReport(t *testing.T) {
	username := newCLITestUser(t)
	if _, err := runUsers(t, "edit", username, "--quota", "100"); err != nil {
		t.Fatalf("fijando la cuota: %v", err)
	}
	if _, err := runFiles(t, "upload", "--username", username, writeLocalFile(t, "a.bin", 60), "/a.bin"); err != nil {
		t.Fatalf("subiendo: %v", err)
	}

	out, err := runUsers(t, "quota")
	if err != nil {
		t.Fatalf("users quota falló: %v (salida: %s)", err, out)
	}
	for _, want := range []string{"USUARIO", "USADO", "CUOTA", "%", "ORIGEN", username, "60 B", "100 B", "60.0%", "usuario"} {
		if !strings.Contains(out, want) {
			t.Errorf("la salida de users quota no contiene %q:\n%s", want, out)
		}
	}

	// Con un usuario concreto solo sale ese; --detail añade el desglose.
	out, err = runUsers(t, "quota", username, "--detail")
	if err != nil {
		t.Fatalf("users quota <usuario> --detail falló: %v (salida: %s)", err, out)
	}
	for _, want := range []string{"ARCHIVOS", "PAPELERA", "VERSIONES", username} {
		if !strings.Contains(out, want) {
			t.Errorf("la salida detallada no contiene %q:\n%s", want, out)
		}
	}
	if _, err := runUsers(t, "quota", "no-existe"); err == nil {
		t.Error("users quota de un usuario inexistente debe fallar")
	}
}

// El informe dice de dónde sale el límite: usuario, grupo o global.
func TestUsersQuotaReportShowsWhereTheLimitComesFrom(t *testing.T) {
	username := newCLITestUser(t)
	t.Setenv("NEXUSCLOUD_STORAGE_DEFAULT_QUOTA_BYTES", "1000")

	out, _ := runUsers(t, "quota", username)
	if !strings.Contains(out, "global") || !strings.Contains(out, "1000 B") {
		t.Errorf("con solo la cuota global la salida debería decir global y 1000 B:\n%s", out)
	}

	if _, err := runUsers(t, "group", "create", "Familia", "--quota", "900"); err != nil {
		t.Fatal(err)
	}
	if _, err := runUsers(t, "group", "add-member", username, "Familia"); err != nil {
		t.Fatal(err)
	}
	out, _ = runUsers(t, "quota", username)
	if !strings.Contains(out, "grupo:Familia") || !strings.Contains(out, "900 B") {
		t.Errorf("con un grupo con cuota debería decir grupo:Familia y 2000 B:\n%s", out)
	}

	if _, err := runUsers(t, "edit", username, "--quota", "unlimited"); err != nil {
		t.Fatal(err)
	}
	out, _ = runUsers(t, "quota", username)
	if !strings.Contains(out, "ilimitada") || !strings.Contains(out, "usuario") {
		t.Errorf("con ilimitada propia debería decir ilimitada y usuario:\n%s", out)
	}
}
