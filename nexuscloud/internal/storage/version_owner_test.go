package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

// file_versions guarda el propietario de cada versión, copiado de su archivo
// (migración 0012, ADR-036): así el uso de versiones de un usuario es una lectura
// de índice y no un JOIN con todos sus archivos. El repositorio lo toma de la
// propia fila de files, no de quien llama: no puede haber una versión con un
// propietario distinto del de su archivo.

func TestCreateVersionTakesTheOwnerFromTheFile(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	f, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("uno"))})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	v := &FileVersion{
		ID: idgen.New(), FileID: f.ID, VersionNum: 1, SizeBytes: 3, SHA256: "h", MimeType: "text/plain",
		StorageKey: "k", CreatedAt: time.Now().UTC(),
	}
	if err := env.versions.CreateVersion(ctx, v); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	got, err := env.versions.ListVersions(ctx, f.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListVersions = %d versiones (%v), esperado 1", len(got), err)
	}
	if got[0].OwnerID != owner {
		t.Errorf("OwnerID de la versión = %q, esperado %q (el del archivo)", got[0].OwnerID, owner)
	}
}

func TestCreateVersionOfAMissingFileFails(t *testing.T) {
	env := newTestEnv(t, true)
	v := &FileVersion{
		ID: idgen.New(), FileID: "no-existe", VersionNum: 1, SizeBytes: 3, SHA256: "h", MimeType: "text/plain",
		StorageKey: "k", CreatedAt: time.Now().UTC(),
	}
	if err := env.versions.CreateVersion(context.Background(), v); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("CreateVersion de un archivo inexistente = %v, esperado ErrFileNotFound", err)
	}
}
