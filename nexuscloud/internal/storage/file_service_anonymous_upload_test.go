package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// Subida anónima (§38, ADR-039): calca el patrón polimórfico... no, este NO
// es polimórfico (siempre carpeta) -- calca en cambio el patrón de shares
// para token/ciclo de vida, pero sin ningún método de navegación: es la
// garantía estructural de "sin acceso al resto del contenido".

func TestCreateAnonymousUploadLinkOnOwnDirectory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")

	link, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "Fotos de la boda", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}
	if token == "" {
		t.Error("el token en claro no debería estar vacío")
	}
	if link.TokenHash == token || link.TokenHash != hashShareToken(token) {
		t.Error("solo debe persistirse el hash del token, nunca el valor en claro")
	}
	if link.DirectoryID != dir.ID || link.Label != "Fotos de la boda" {
		t.Errorf("enlace creado = %+v", link)
	}
	if link.IsRevoked() || link.IsExpired(time.Now().UTC()) {
		t.Error("un enlace recién creado no debe estar revocado ni expirado")
	}
}

func TestCreateAnonymousUploadLinkRejectsDirectoryOwnedByAnotherUser(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	stranger := env.user(t, "extrana")
	dir := mkdir(t, env, owner, "Buzon")

	if _, _, err := env.svc.CreateAnonymousUploadLink(ctx, stranger, dir.ID, "", nil, nil); !errors.Is(err, ErrAnonymousUploadForbidden) {
		t.Errorf("crear sobre la carpeta de otro = %v, esperado ErrAnonymousUploadForbidden", err)
	}
}

func TestAnonymousUploadRequiresEnabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")

	// Sin WithAnonymousUploads en absoluto.
	bare := NewFileService(env.files, env.directories, env.versions, env.shares, env.pools,
		NewPoolProviderResolver(env.pools), &testPasswordHasher{}, true, true, 10, 0, 0, true, true)
	if _, _, err := bare.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil); !errors.Is(err, ErrAnonymousUploadDisabled) {
		t.Errorf("sin WithAnonymousUploads: err = %v, esperado ErrAnonymousUploadDisabled", err)
	}

	// Con el repositorio conectado pero enabled=false (sharing.anonymousUploadEnabled=false).
	disabled := NewFileService(env.files, env.directories, env.versions, env.shares, env.pools,
		NewPoolProviderResolver(env.pools), &testPasswordHasher{}, true, true, 10, 0, 0, true, true,
		WithAnonymousUploads(env.anonymousUploads, false))
	if _, _, err := disabled.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil); !errors.Is(err, ErrAnonymousUploadDisabled) {
		t.Errorf("con enabled=false: err = %v, esperado ErrAnonymousUploadDisabled", err)
	}
}

func TestUploadViaAnonymousLinkSucceedsAndCountsUpload(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")
	link, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	content := blob(7, 100)
	meta, err := env.svc.UploadViaAnonymousLink(ctx, token, "regalo.jpg", bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("UploadViaAnonymousLink: %v", err)
	}
	if meta.OwnerID != owner || meta.ParentPath != "/Buzon" || meta.Name != "regalo.jpg" {
		t.Errorf("archivo subido = %+v", meta)
	}

	links, err := env.svc.ListAnonymousUploadLinks(ctx, owner)
	if err != nil || len(links) != 1 || links[0].UploadCount != 1 {
		t.Errorf("upload_count tras subir = %+v (%v), esperado 1", links, err)
	}
	_ = link
}

func TestUploadViaAnonymousLinkRejectsRevokedOrExpired(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")

	revoked, revokedToken, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink (revocado): %v", err)
	}
	if err := env.svc.RevokeAnonymousUploadLink(ctx, owner, revoked.ID); err != nil {
		t.Fatalf("RevokeAnonymousUploadLink: %v", err)
	}
	if _, err := env.svc.UploadViaAnonymousLink(ctx, revokedToken, "x.txt", bytes.NewReader(blob(1, 1)), 1); !errors.Is(err, ErrAnonymousUploadRevoked) {
		t.Errorf("subir con un enlace revocado = %v, esperado ErrAnonymousUploadRevoked", err)
	}

	past := time.Now().UTC().Add(-time.Hour)
	_, expiredToken, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, &past)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink (expirado): %v", err)
	}
	if _, err := env.svc.UploadViaAnonymousLink(ctx, expiredToken, "x.txt", bytes.NewReader(blob(1, 1)), 1); !errors.Is(err, ErrAnonymousUploadExpired) {
		t.Errorf("subir con un enlace expirado = %v, esperado ErrAnonymousUploadExpired", err)
	}
}

func TestUploadViaAnonymousLinkRejectsUnknownToken(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)

	if _, err := env.svc.UploadViaAnonymousLink(ctx, "token-que-no-existe-de-verdad", "x.txt", bytes.NewReader(blob(1, 1)), 1); !errors.Is(err, ErrAnonymousUploadNotFound) {
		t.Errorf("token inexistente: err = %v, esperado ErrAnonymousUploadNotFound", err)
	}
}

func TestUploadViaAnonymousLinkRespectsMaxSize(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")
	limit := int64(10)
	_, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", &limit, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	content := blob(1, 11) // 1 byte por encima del límite
	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "grande.bin", bytes.NewReader(content), int64(len(content))); !errors.Is(err, ErrAnonymousUploadTooLarge) {
		t.Errorf("subir por encima del límite = %v, esperado ErrAnonymousUploadTooLarge", err)
	}

	result, err := env.svc.List(ctx, owner, "/Buzon")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Files) != 0 {
		t.Errorf("no debería haber quedado ningún archivo tras el corte: %+v", result.Files)
	}
}

func TestUploadViaAnonymousLinkNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")
	_, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "mismo.txt", bytes.NewReader(blob(1, 5)), 5); err != nil {
		t.Fatalf("primera subida: %v", err)
	}
	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "mismo.txt", bytes.NewReader(blob(2, 5)), 5); !errors.Is(err, ErrDestinationOccupied) {
		t.Errorf("segunda subida con el mismo nombre = %v, esperado ErrDestinationOccupied", err)
	}
}

func TestUploadViaAnonymousLinkRejectsTrashedDirectory(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")
	_, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory (a la papelera): %v", err)
	}

	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "x.txt", bytes.NewReader(blob(1, 1)), 1); !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("subir a una carpeta trasheada = %v, esperado ErrDirectoryNotFound", err)
	}
}

func TestResolveAnonymousUploadForAccessRejectsDisabledGlobally(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")
	_, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	// Mismo criterio que ResolvePublicShare/publicLinksEnabled: apagar el
	// interruptor global corta también los enlaces YA creados, no solo los
	// nuevos.
	disabled := NewFileService(env.files, env.directories, env.versions, env.shares, env.pools,
		NewPoolProviderResolver(env.pools), &testPasswordHasher{}, true, true, 10, 0, 0, true, true,
		WithAnonymousUploads(env.anonymousUploads, false))
	if _, err := disabled.ResolveAnonymousUploadForAccess(ctx, token); !errors.Is(err, ErrAnonymousUploadDisabled) {
		t.Errorf("resolver con la función desactivada = %v, esperado ErrAnonymousUploadDisabled", err)
	}
}

func TestRevokeAnonymousUploadLinkRequiresOwnerIDOR(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	stranger := env.user(t, "extrana")
	dir := mkdir(t, env, owner, "Buzon")
	link, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	if err := env.svc.RevokeAnonymousUploadLink(ctx, stranger, link.ID); !errors.Is(err, ErrAnonymousUploadNotFound) {
		t.Errorf("revocar el enlace de otro = %v, esperado ErrAnonymousUploadNotFound", err)
	}
	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "x.txt", bytes.NewReader(blob(1, 1)), 1); err != nil {
		t.Errorf("tras el intento fallido, el enlace debería seguir funcionando: %v", err)
	}

	if err := env.svc.RevokeAnonymousUploadLink(ctx, owner, link.ID); err != nil {
		t.Fatalf("RevokeAnonymousUploadLink por el propietario: %v", err)
	}
}

func TestListAnonymousUploadLinksIsolatedPerOwner(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	ana := env.user(t, "ana")
	bea := env.user(t, "bea")
	dirAna := mkdir(t, env, ana, "BuzonAna")
	dirBea := mkdir(t, env, bea, "BuzonBea")
	if _, _, err := env.svc.CreateAnonymousUploadLink(ctx, ana, dirAna.ID, "de ana", nil, nil); err != nil {
		t.Fatalf("CreateAnonymousUploadLink ana: %v", err)
	}
	if _, _, err := env.svc.CreateAnonymousUploadLink(ctx, bea, dirBea.ID, "de bea", nil, nil); err != nil {
		t.Fatalf("CreateAnonymousUploadLink bea: %v", err)
	}

	anaLinks, err := env.svc.ListAnonymousUploadLinks(ctx, ana)
	if err != nil || len(anaLinks) != 1 || anaLinks[0].Label != "de ana" {
		t.Errorf("enlaces de ana = %+v (%v), esperado solo el suyo", anaLinks, err)
	}
}
