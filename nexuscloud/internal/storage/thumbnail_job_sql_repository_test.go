package storage

import (
	"context"
	"errors"
	"testing"
)

// Miniaturas (§34, ADR-041): la tabla thumbnail_jobs es una cola interna,
// no un recurso de cara al usuario -- estos tests ejercitan el
// repositorio directamente (igual que pool_sql_repository_test.go),
// reutilizando newTestEnv/uploadFile para tener un file_id real que
// satisfaga la FK ON DELETE CASCADE.

func TestThumbnailJobUpsertPendingCreaJobNuevo(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)

	job, err := repo.UpsertPending(context.Background(), f.ID, "sha-abc", ThumbnailKindImage)
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if job.FileID != f.ID || job.SHA256 != "sha-abc" || job.Kind != ThumbnailKindImage {
		t.Errorf("job = %+v, campos inesperados", job)
	}
	if job.Status != ThumbnailJobPending || job.Attempts != 0 || job.LastError != "" {
		t.Errorf("job nuevo = %+v, esperado pending/0/sin error", job)
	}
}

func TestThumbnailJobUpsertPendingReinicioSobreExistente(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()

	first, err := repo.UpsertPending(ctx, f.ID, "sha-v1", ThumbnailKindImage)
	if err != nil {
		t.Fatalf("primer UpsertPending: %v", err)
	}
	// Simula que falló un par de veces antes de que el archivo se resubiera.
	if _, err := repo.MarkFailedAttempt(ctx, first.ID, "boom", 5); err != nil {
		t.Fatalf("MarkFailedAttempt: %v", err)
	}

	second, err := repo.UpsertPending(ctx, f.ID, "sha-v2", ThumbnailKindImage)
	if err != nil {
		t.Fatalf("segundo UpsertPending: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("UNIQUE(file_id) debería reutilizar la misma fila, first.ID=%s second.ID=%s", first.ID, second.ID)
	}
	if second.SHA256 != "sha-v2" || second.Status != ThumbnailJobPending || second.Attempts != 0 {
		t.Errorf("job reiniciado = %+v, esperado sha-v2/pending/0", second)
	}
}

func TestThumbnailJobNextPendingOrdenYVacio(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()

	if job, err := repo.NextPending(ctx); err != nil || job != nil {
		t.Fatalf("NextPending sin jobs = (%v, %v), esperado (nil, nil)", job, err)
	}

	fA := uploadFile(t, env, owner, "a.jpg")
	fB := uploadFile(t, env, owner, "b.jpg")
	if _, err := repo.UpsertPending(ctx, fA.ID, "sha-a", ThumbnailKindImage); err != nil {
		t.Fatalf("UpsertPending a: %v", err)
	}
	if _, err := repo.UpsertPending(ctx, fB.ID, "sha-b", ThumbnailKindImage); err != nil {
		t.Fatalf("UpsertPending b: %v", err)
	}

	job, err := repo.NextPending(ctx)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if job == nil || job.FileID != fA.ID {
		t.Errorf("NextPending = %+v, esperado el más antiguo (a)", job)
	}
}

func TestThumbnailJobMarkDoneBorraLaFila(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()

	job, err := repo.UpsertPending(ctx, f.ID, "sha-abc", ThumbnailKindImage)
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := repo.MarkDone(ctx, job.ID); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	next, err := repo.NextPending(ctx)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if next != nil {
		t.Errorf("tras MarkDone no debería quedar ningún job pendiente, quedó %+v", next)
	}
}

func TestThumbnailJobMarkFailedAttemptHastaFailed(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()
	const maxAttempts = 3

	job, err := repo.UpsertPending(ctx, f.ID, "sha-abc", ThumbnailKindImage)
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}

	for i := 1; i < maxAttempts; i++ {
		becameFailed, err := repo.MarkFailedAttempt(ctx, job.ID, "corrupto", maxAttempts)
		if err != nil {
			t.Fatalf("MarkFailedAttempt intento %d: %v", i, err)
		}
		if becameFailed {
			t.Errorf("intento %d: becameFailed=true antes de agotar maxAttempts=%d", i, maxAttempts)
		}
	}

	becameFailed, err := repo.MarkFailedAttempt(ctx, job.ID, "corrupto definitivo", maxAttempts)
	if err != nil {
		t.Fatalf("MarkFailedAttempt final: %v", err)
	}
	if !becameFailed {
		t.Error("al alcanzar maxAttempts, becameFailed debería ser true")
	}

	failed, err := repo.ListByStatus(ctx, ThumbnailJobFailed, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus failed: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != job.ID || failed[0].Attempts != maxAttempts || failed[0].LastError != "corrupto definitivo" {
		t.Errorf("ListByStatus(failed) = %+v, esperado un job con attempts=%d y el último error", failed, maxAttempts)
	}

	pending, err := repo.ListByStatus(ctx, ThumbnailJobPending, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus pending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("ListByStatus(pending) = %+v, esperado vacío tras pasar a failed", pending)
	}
}

// TestThumbnailJobListByStatusLimitCeroUsaDefecto cubre una regresión real
// (§34, ADR-041, encontrada por el test de integración de
// GET /admin/thumbnail-jobs): limit=0 es lo que produce
// strconv.Atoi("") cuando el llamador HTTP no manda ?limit=, y en SQL
// "LIMIT 0" significa devolver CERO filas, no "sin límite" -- exactamente
// el mismo caso que audit.SQLRepository.ListEvents ya cubre con
// `if limit <= 0 { limit = 100 }`. Antes de este fix, GET
// /admin/thumbnail-jobs sin ?limit= devolvía siempre una lista vacía por
// mucho que hubiera jobs pendientes.
func TestThumbnailJobListByStatusLimitCeroUsaDefecto(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()

	if _, err := repo.UpsertPending(ctx, f.ID, "sha-abc", ThumbnailKindImage); err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}

	pending, err := repo.ListByStatus(ctx, ThumbnailJobPending, 0, 0)
	if err != nil {
		t.Fatalf("ListByStatus con limit=0: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("ListByStatus(pending, limit=0) = %d jobs, esperado 1 (limit=0 debe tratarse como \"por defecto\", nunca como \"cero filas\")", len(pending))
	}
}

func TestThumbnailJobGetByFileIDNuncaResetea(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()

	if _, err := repo.GetByFileID(ctx, f.ID); !errors.Is(err, ErrThumbnailJobNotFound) {
		t.Errorf("GetByFileID antes de crear ningún job = %v, esperado ErrThumbnailJobNotFound", err)
	}

	created, err := repo.UpsertPending(ctx, f.ID, "sha-abc", ThumbnailKindImage)
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if _, err := repo.MarkFailedAttempt(ctx, created.ID, "fallo 1", 5); err != nil {
		t.Fatalf("MarkFailedAttempt: %v", err)
	}

	got, err := repo.GetByFileID(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetByFileID: %v", err)
	}
	if got.Attempts != 1 {
		t.Errorf("GetByFileID.Attempts = %d, esperado 1 -- GetByFileID nunca debe resetear un job existente", got.Attempts)
	}
}

func TestThumbnailJobBorradoDeArchivoBorraElJobPorCascade(t *testing.T) {
	env := newTestEnv(t, false)
	owner := env.user(t, "duena")
	f := uploadFile(t, env, owner, "foto.jpg")
	repo := NewSQLThumbnailJobRepository(env.conn)
	ctx := context.Background()

	if _, err := repo.UpsertPending(ctx, f.ID, "sha-abc", ThumbnailKindImage); err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	// Borrado PARA SIEMPRE (no a la papelera): la fila de files desaparece
	// de verdad, así que ON DELETE CASCADE debe limpiar el job huérfano.
	if err := env.svc.PermanentlyDeleteFile(ctx, owner, f.ID); err != nil {
		t.Fatalf("PermanentlyDeleteFile: %v", err)
	}
	job, err := repo.NextPending(ctx)
	if err != nil {
		t.Fatalf("NextPending: %v", err)
	}
	if job != nil {
		t.Errorf("tras borrar el archivo para siempre, el job debería desaparecer por CASCADE; quedó %+v", job)
	}
}

func TestThumbnailKindForMimeType(t *testing.T) {
	cases := []struct {
		mimeType string
		want     ThumbnailKind
	}{
		{"image/jpeg", ThumbnailKindImage},
		{"image/png", ThumbnailKindImage},
		{"video/mp4", ThumbnailKindVideo},
		{"application/pdf", ThumbnailKindPDF},
		{"text/plain", ""},
		{"application/octet-stream", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ThumbnailKindForMimeType(tc.mimeType); got != tc.want {
			t.Errorf("ThumbnailKindForMimeType(%q) = %q, esperado %q", tc.mimeType, got, tc.want)
		}
	}
}
