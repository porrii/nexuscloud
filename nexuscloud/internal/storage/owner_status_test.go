package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/porrii/nexuscloud/internal/users"
)

// Enlaces públicos y de subida anónima de cuentas desactivadas (ADR-043):
// el enlace deja de existir para quien lo use mientras la cuenta esté
// desactivada, sin tocar sus filas, y vuelve a funcionar al reactivarla.

func setUserStatus(t *testing.T, env *testEnv, userID string, status users.Status) {
	t.Helper()
	ctx := context.Background()
	u, err := env.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	u.Status = status
	if err := env.userRepo.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
}

func TestPublicFileLinkOfDisabledOwnerIsNotFoundUntilReactivated(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	meta := uploadFile(t, env, owner, "informe.pdf")
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true,
	})
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	setUserStatus(t, env, owner, users.StatusDisabled)

	if _, err := env.svc.ResolvePublicShare(ctx, token); !errors.Is(err, ErrShareNotFound) {
		t.Errorf("ResolvePublicShare = %v, esperado ErrShareNotFound (sin revelar que existió)", err)
	}
	if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "", ""); !errors.Is(err, ErrShareNotFound) {
		t.Errorf("DownloadViaPublicShare = %v, esperado ErrShareNotFound", err)
	}
	share, err := env.shares.GetShareByTokenHash(ctx, hashShareToken(token))
	if err != nil {
		t.Fatalf("GetShareByTokenHash: %v", err)
	}
	if share.DownloadCount != 0 {
		t.Errorf("download_count = %d, esperado 0: un acceso rechazado no consume descargas", share.DownloadCount)
	}
	if share.IsRevoked() {
		t.Error("desactivar la cuenta no debe revocar el enlace (ADR-043 Decisión 5)")
	}

	setUserStatus(t, env, owner, users.StatusActive)

	_, rc, err := env.svc.DownloadViaPublicShare(ctx, token, "", "")
	if err != nil {
		t.Fatalf("tras reactivar, DownloadViaPublicShare = %v, esperado éxito", err)
	}
	rc.Close()
}

func TestPublicDirectoryLinkOfDisabledOwnerRejectsBrowseAndUploadBeforePassword(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	dir := mkdir(t, env, owner, "Compartida")
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceIsDirectory: true, ResourceID: dir.ID, Type: ShareTypeLink,
		CanDownload: true, CanUpload: true, Password: "clave-del-enlace",
	})
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	setUserStatus(t, env, owner, users.StatusDisabled)

	// Con una contraseña incorrecta: el rechazo llega antes de verificarla,
	// así que la respuesta no distingue «existe pero la clave no vale».
	if _, err := env.svc.BrowsePublicShare(ctx, token, "mala", ""); !errors.Is(err, ErrShareNotFound) {
		t.Errorf("BrowsePublicShare = %v, esperado ErrShareNotFound", err)
	}
	_, err = env.svc.UploadViaPublicShare(ctx, PublicUploadInput{
		Token: token, Password: "clave-del-enlace", Name: "intruso.txt", Content: bytes.NewReader([]byte("x")),
	})
	if !errors.Is(err, ErrShareNotFound) {
		t.Errorf("UploadViaPublicShare = %v, esperado ErrShareNotFound", err)
	}
	listing, err := env.svc.List(ctx, owner, "/Compartida")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listing.Files) != 0 {
		t.Errorf("la carpeta tiene %d archivos, esperado 0: la subida rechazada no debe escribir nada", len(listing.Files))
	}
}

func TestAnonymousUploadOfDisabledOwnerIsNotFoundUntilReactivated(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "duena")
	dir := mkdir(t, env, owner, "Buzon")
	_, token, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	setUserStatus(t, env, owner, users.StatusDisabled)

	if _, err := env.svc.ResolveAnonymousUploadForAccess(ctx, token); !errors.Is(err, ErrAnonymousUploadNotFound) {
		t.Errorf("ResolveAnonymousUploadForAccess = %v, esperado ErrAnonymousUploadNotFound", err)
	}
	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "a.txt", bytes.NewReader([]byte("x")), 1); !errors.Is(err, ErrAnonymousUploadNotFound) {
		t.Errorf("UploadViaAnonymousLink = %v, esperado ErrAnonymousUploadNotFound", err)
	}
	link, err := env.anonymousUploads.GetLinkByTokenHash(ctx, hashShareToken(token))
	if err != nil {
		t.Fatalf("GetLinkByTokenHash: %v", err)
	}
	if link.UploadCount != 0 || link.IsRevoked() {
		t.Errorf("enlace tras el rechazo = %+v, esperado sin subidas y sin revocar", link)
	}

	setUserStatus(t, env, owner, users.StatusActive)

	if _, err := env.svc.UploadViaAnonymousLink(ctx, token, "a.txt", bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatalf("tras reactivar, UploadViaAnonymousLink = %v, esperado éxito", err)
	}
}

// Las comparticiones con usuarios y grupos de una cuenta desactivada se
// mantienen (ADR-043 Decisión 6): sus destinatarios son cuentas activas.
func TestUserShareOfDisabledOwnerKeepsWorking(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	meta := uploadFile(t, env, owner, "informe.pdf")
	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true,
	}); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	setUserStatus(t, env, owner, users.StatusDisabled)

	_, rc, err := env.svc.Download(ctx, target, meta.ID)
	if err != nil {
		t.Fatalf("Download del destinatario con el propietario desactivado = %v, esperado éxito", err)
	}
	rc.Close()
}

type failingOwnerStatus struct{}

func (failingOwnerStatus) IsActive(context.Context, string) (bool, error) {
	return false, errors.New("base de datos caída")
}

// Fallar cerrado (ADR-043 Decisión 4): sin comprobador, o si la consulta
// falla, no se resuelve ningún enlace.
func TestPublicResolutionFailsClosedWithoutUsableOwnerStatus(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	meta := uploadFile(t, env, owner, "informe.pdf")
	_, shareToken, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true,
	})
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	dir := mkdir(t, env, owner, "Buzon")
	_, anonToken, err := env.svc.CreateAnonymousUploadLink(ctx, owner, dir.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("CreateAnonymousUploadLink: %v", err)
	}

	build := func(opts ...FileServiceOption) *FileService {
		opts = append([]FileServiceOption{WithAnonymousUploads(env.anonymousUploads, true)}, opts...)
		return NewFileService(env.files, env.directories, env.versions, env.shares, env.pools,
			NewPoolProviderResolver(env.pools), &testPasswordHasher{}, true, true, 10, 0, 0, true, true, opts...)
	}

	cases := []struct {
		name string
		svc  *FileService
		want error
	}{
		{"sin WithOwnerStatus", build(), ErrOwnerStatusUnavailable},
		{"comprobador que falla", build(WithOwnerStatus(failingOwnerStatus{})), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.svc.ResolvePublicShare(ctx, shareToken)
			if err == nil || errors.Is(err, ErrShareNotFound) {
				t.Errorf("ResolvePublicShare = %v, esperado un error interno", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("ResolvePublicShare = %v, esperado %v", err, tc.want)
			}
			_, err = tc.svc.ResolveAnonymousUploadForAccess(ctx, anonToken)
			if err == nil || errors.Is(err, ErrAnonymousUploadNotFound) {
				t.Errorf("ResolveAnonymousUploadForAccess = %v, esperado un error interno", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("ResolveAnonymousUploadForAccess = %v, esperado %v", err, tc.want)
			}
		})
	}
}
