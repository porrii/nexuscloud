package storage

import (
	"bytes"
	"context"
	"testing"
)

// La coherencia de file_versions.owner_id con files.owner_id (migración 0012, ADR-036) a través
// de los flujos reales del servicio, comparada con el JOIN de antes de la 0012 y medida con Usage.

// versionOwnerMismatches cuenta las versiones cuyo propietario no coincide con el
// de su archivo: siempre debe ser 0.
func versionOwnerMismatches(t *testing.T, env *testEnv) int64 {
	t.Helper()
	var n int64
	if err := env.conn.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM file_versions v JOIN files f ON f.id = v.file_id WHERE v.owner_id <> f.owner_id`).Scan(&n); err != nil {
		t.Fatalf("comprobando la coherencia de los propietarios: %v", err)
	}
	return n
}

// versionBytesByJoin es la verdad de referencia: lo que suman las versiones de un
// propietario según el JOIN con sus archivos (la forma de calcularlo antes de la 0012).
func versionBytesByJoin(t *testing.T, env *testEnv, owner string) int64 {
	t.Helper()
	var n int64
	if err := env.conn.QueryRowContext(context.Background(),
		`SELECT COALESCE(SUM(v.size_bytes), 0) FROM file_versions v JOIN files f ON f.id = v.file_id WHERE f.owner_id = ?`, owner).Scan(&n); err != nil {
		t.Fatalf("sumando versiones por JOIN: %v", err)
	}
	return n
}

// Tras sobrescribir, restaurar versiones y borrar para siempre, la columna
// desnormalizada da EXACTAMENTE lo mismo que el JOIN y nunca hay un propietario
// distinto del del archivo.
func TestVersionOwnerStaysConsistentThroughTheServiceFlows(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	env.withQuotas() // para poder pedir Usage
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")

	check := func(when string) {
		t.Helper()
		if n := versionOwnerMismatches(t, env); n != 0 {
			t.Errorf("%s: %d versiones con un propietario distinto del de su archivo", when, n)
		}
		for name, owner := range map[string]string{"ana": ana, "bea": bea} {
			if got, want := mustUsage(t, env, owner).VersionsBytes, versionBytesByJoin(t, env, owner); got != want {
				t.Errorf("%s: versiones de %s por la columna = %d, por JOIN = %d", when, name, got, want)
			}
		}
	}

	up := func(owner, name string, content []byte) *FileMeta {
		t.Helper()
		m, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: name, Content: bytes.NewReader(content)})
		if err != nil {
			t.Fatalf("Upload %s: %v", name, err)
		}
		return m
	}

	a := up(ana, "a.txt", blob(1, 10))
	up(ana, "a.txt", blob(2, 20)) // versión de 10
	up(ana, "a.txt", blob(3, 30)) // versión de 20
	b := up(bea, "b.txt", blob(4, 5))
	up(bea, "b.txt", blob(5, 6)) // versión de 5
	check("tras sobrescribir")
	if got := mustUsage(t, env, ana).VersionsBytes; got != 30 {
		t.Fatalf("versiones de ana = %d, esperado 30 (10 + 20)", got)
	}
	if got := mustUsage(t, env, bea).VersionsBytes; got != 5 {
		t.Fatalf("versiones de bea = %d, esperado 5", got)
	}

	if _, err := env.svc.RestoreVersion(ctx, ana, a.ID, 1); err != nil {
		t.Fatalf("RestoreVersion: %v", err)
	}
	check("tras restaurar una versión")

	if err := env.svc.PermanentlyDeleteFile(ctx, bea, b.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile: %v", err)
	}
	check("tras borrar un archivo para siempre")
	if got := mustUsage(t, env, bea).VersionsBytes; got != 0 {
		t.Errorf("versiones de bea tras borrar su archivo = %d, esperado 0 (el historial no sobrevive a su archivo)", got)
	}
}
