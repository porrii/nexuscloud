package storage

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/porrii/nexuscloud/internal/idgen"
)

func TestDownloadViaUserShareGrantsAccessOnlyToTarget(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	stranger := env.user(t, "stranger")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "informe.txt", Content: bytes.NewReader([]byte("contenido"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	share, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true,
	})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}
	if token != "" {
		t.Errorf("token = %q, esperado vacío para un share que no es de tipo enlace", token)
	}
	if share.CanUpload {
		t.Error("CanUpload debería forzarse a false en un share de tipo usuario (§37: opciones de permiso son de Enlaces)")
	}

	if _, rc, err := env.svc.Download(ctx, target, meta.ID); err != nil {
		t.Fatalf("Download del destinatario del share falló: %v", err)
	} else {
		rc.Close()
	}

	if _, _, err := env.svc.Download(ctx, stranger, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("Download de un tercero ajeno al share = %v, esperado ErrForbidden", err)
	}
}

func TestDownloadViaGroupShareGrantsAccessToMembers(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	member := env.user(t, "member")
	nonMember := env.user(t, "non-member")
	groupID := env.group(t, "Marketing")
	env.addToGroup(t, member, groupID)

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "campaña.pdf", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeGroup, TargetGroupID: groupID, CanDownload: true,
	}); err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}

	if _, rc, err := env.svc.Download(ctx, member, meta.ID); err != nil {
		t.Fatalf("Download de un miembro del grupo falló: %v", err)
	} else {
		rc.Close()
	}
	if _, _, err := env.svc.Download(ctx, nonMember, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("Download de quien no pertenece al grupo = %v, esperado ErrForbidden", err)
	}
}

// TestSharedDirectoryAccessReachesNestedFile comprueba que compartir una
// carpeta da acceso a todo su contenido, incluida una subcarpeta dos
// niveles por debajo (§37): el recorrido de ancestros debe alcanzarla.
func TestSharedDirectoryAccessReachesNestedFile(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")

	docs, err := env.svc.Mkdir(ctx, owner, "/", "Documentos", "")
	if err != nil {
		t.Fatalf("Mkdir(/Documentos) falló: %v", err)
	}
	sub, err := env.svc.Mkdir(ctx, owner, "/Documentos", "Contratos", "")
	if err != nil {
		t.Fatalf("Mkdir(/Documentos/Contratos) falló: %v", err)
	}
	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Documentos/Contratos", Name: "acuerdo.pdf", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload anidado falló: %v", err)
	}

	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceIsDirectory: true, ResourceID: docs.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true,
	}); err != nil {
		t.Fatalf("CreateShare de carpeta falló: %v", err)
	}

	// Descargar el archivo anidado dos niveles por debajo debe funcionar vía
	// el recorrido de ancestros, aunque no haya ningún share directo sobre
	// "Contratos" ni sobre el archivo.
	if _, rc, err := env.svc.Download(ctx, target, meta.ID); err != nil {
		t.Fatalf("Download de archivo anidado en carpeta compartida falló: %v", err)
	} else {
		rc.Close()
	}

	// Navegar también debe funcionar en ambos niveles.
	top, err := env.svc.ListSharedDirectory(ctx, target, docs.ID)
	if err != nil {
		t.Fatalf("ListSharedDirectory(Documentos) falló: %v", err)
	}
	if len(top.Directories) != 1 || top.Directories[0].ID != sub.ID {
		t.Fatalf("ListSharedDirectory(Documentos) = %+v, esperado solo Contratos", top.Directories)
	}
	nested, err := env.svc.ListSharedDirectory(ctx, target, sub.ID)
	if err != nil {
		t.Fatalf("ListSharedDirectory(Contratos) falló: %v", err)
	}
	if len(nested.Files) != 1 || nested.Files[0].ID != meta.ID {
		t.Fatalf("ListSharedDirectory(Contratos) = %+v, esperado solo acuerdo.pdf", nested.Files)
	}
}

func TestRevokeShareRemovesAccess(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")

	meta, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}
	share, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}
	if _, rc, err := env.svc.Download(ctx, target, meta.ID); err != nil {
		t.Fatalf("Download antes de revocar falló: %v", err)
	} else {
		rc.Close()
	}

	if err := env.svc.RevokeShare(ctx, owner, share.ID); err != nil {
		t.Fatalf("RevokeShare falló: %v", err)
	}
	if _, _, err := env.svc.Download(ctx, target, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("Download tras revocar = %v, esperado ErrForbidden", err)
	}
}

func TestRevokeShareRequiresOwnership(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")

	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})
	share, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}

	if err := env.svc.RevokeShare(ctx, target, share.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("RevokeShare por el destinatario (no propietario) = %v, esperado ErrForbidden", err)
	}
}

func TestPublicLinkPasswordProtection(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")

	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "secreto.txt", Content: bytes.NewReader([]byte("x"))})
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true, Password: "correcto123",
	})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}
	if token == "" {
		t.Fatal("token vacío para un share de tipo enlace")
	}

	if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "incorrecta", ""); !errors.Is(err, ErrSharePasswordIncorrect) {
		t.Errorf("err = %v, esperado ErrSharePasswordIncorrect", err)
	}
	if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "", ""); !errors.Is(err, ErrSharePasswordRequired) {
		t.Errorf("err = %v, esperado ErrSharePasswordRequired sin contraseña", err)
	}
	if _, rc, err := env.svc.DownloadViaPublicShare(ctx, token, "correcto123", ""); err != nil {
		t.Fatalf("DownloadViaPublicShare con contraseña correcta falló: %v", err)
	} else {
		rc.Close()
	}
}

func TestPublicLinkExpiredRejectsAccess(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	// CreateShare exige ExpiresAt en el futuro (evita crear un enlace ya
	// caducado por error): para probar el rechazo de un enlace expirado se
	// construye la fila directamente, igual que TestInvitationRedeemRejectsExpired.
	token := "token-de-prueba-expirado"
	past := time.Now().UTC().Add(-time.Hour)
	share := &Share{
		ID: idgen.New(), OwnerID: owner, FileID: meta.ID, Type: ShareTypeLink,
		TokenHash: hashShareToken(token), CanDownload: true, ExpiresAt: &past,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := env.shares.CreateShare(ctx, share); err != nil {
		t.Fatalf("creando share expirado directamente: %v", err)
	}

	if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "", ""); !errors.Is(err, ErrShareExpired) {
		t.Errorf("err = %v, esperado ErrShareExpired", err)
	}
}

func TestPublicLinkExhaustedRejectsAccess(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	one := 1
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true, MaxDownloads: &one,
	})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}

	if _, rc, err := env.svc.DownloadViaPublicShare(ctx, token, "", ""); err != nil {
		t.Fatalf("primera descarga (dentro del límite) falló: %v", err)
	} else {
		rc.Close()
	}
	if _, _, err := env.svc.DownloadViaPublicShare(ctx, token, "", ""); !errors.Is(err, ErrShareExhausted) {
		t.Errorf("segunda descarga tras agotar max_downloads = %v, esperado ErrShareExhausted", err)
	}
}

// TestPublicLinkDownloadCountConcurrencyRespectsLimit lanza descargas
// concurrentes contra un enlace con max_downloads=5 y comprueba que exactamente
// 5 tienen éxito -- el incremento atómico (UPDATE...WHERE + RowsAffected) debe
// impedir que una carrera dé acceso de más al último hueco disponible.
func TestPublicLinkDownloadCountConcurrencyRespectsLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	limit := 5
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true, MaxDownloads: &limit,
	})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}

	const attempts = 20
	var successes int64
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			_, rc, err := env.svc.DownloadViaPublicShare(ctx, token, "", "")
			if err == nil {
				rc.Close()
				atomic.AddInt64(&successes, 1)
			} else if !errors.Is(err, ErrShareExhausted) {
				t.Errorf("descarga concurrente falló con un error inesperado: %v", err)
			}
		}()
	}
	wg.Wait()

	if int(successes) != limit {
		t.Errorf("descargas exitosas = %d, esperado exactamente %d (max_downloads)", successes, limit)
	}
}

func TestCreateShareRejectsLinkWhenPublicLinksDisabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 10, 0, 0, true, false) // sharing.enabled=true, publicLinksEnabled=false
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanDownload: true,
	}); !errors.Is(err, ErrPublicLinksDisabled) {
		t.Errorf("crear enlace con publicLinksEnabled=false = %v, esperado ErrPublicLinksDisabled", err)
	}

	// La compartición interna (usuario/grupo) es independiente de
	// publicLinksEnabled: debe seguir funcionando.
	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true,
	}); err != nil {
		t.Errorf("crear share de usuario con publicLinksEnabled=false falló inesperadamente: %v", err)
	}
}

func TestCreateShareRejectsAllWhenSharingDisabled(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvFull(t, true, true, 10, 0, 0, false, false)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true,
	}); !errors.Is(err, ErrSharingDisabled) {
		t.Errorf("crear share con sharing.enabled=false = %v, esperado ErrSharingDisabled", err)
	}
}

func TestCreateShareRejectsUploadOnFileResource(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	if _, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceID: meta.ID, Type: ShareTypeLink, CanUpload: true,
	}); !errors.Is(err, ErrInvalidShare) {
		t.Errorf("enlace de subida sobre un archivo (no carpeta) = %v, esperado ErrInvalidShare", err)
	}
}

func TestUploadViaPublicShareEnforcesSizeLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	dir, err := env.svc.Mkdir(ctx, owner, "/", "Buzon", "")
	if err != nil {
		t.Fatalf("Mkdir falló: %v", err)
	}

	limit := int64(10)
	_, token, err := env.svc.CreateShare(ctx, owner, CreateShareInput{
		ResourceIsDirectory: true, ResourceID: dir.ID, Type: ShareTypeLink,
		CanUpload: true, MaxUploadSizeBytes: &limit,
	})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}

	tooLarge := bytes.Repeat([]byte("x"), 11)
	if _, err := env.svc.UploadViaPublicShare(ctx, PublicUploadInput{
		Token: token, Name: "grande.txt", Content: bytes.NewReader(tooLarge),
	}); !errors.Is(err, ErrShareUploadTooLarge) {
		t.Errorf("subida de 11 bytes con límite 10 = %v, esperado ErrShareUploadTooLarge", err)
	}
	// El archivo que superó el límite no debe haber quedado creado.
	if list, err := env.svc.List(ctx, owner, "/Buzon"); err != nil || len(list.Files) != 0 {
		t.Errorf("List(/Buzon) tras subida rechazada = %+v (err=%v), esperado sin archivos", list, err)
	}

	withinLimit := bytes.Repeat([]byte("y"), 10)
	meta, err := env.svc.UploadViaPublicShare(ctx, PublicUploadInput{
		Token: token, Name: "justo.txt", Content: bytes.NewReader(withinLimit),
	})
	if err != nil {
		t.Fatalf("subida de 10 bytes con límite 10 falló: %v", err)
	}
	if meta.SizeBytes != 10 {
		t.Errorf("SizeBytes = %d, esperado 10", meta.SizeBytes)
	}
}

func TestListSharesExcludesRevoked(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	meta, _ := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("x"))})

	share, _, err := env.svc.CreateShare(ctx, owner, CreateShareInput{ResourceID: meta.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true})
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}

	byMe, err := env.svc.ListSharesByMe(ctx, owner)
	if err != nil || len(byMe) != 1 {
		t.Fatalf("ListSharesByMe antes de revocar = %+v (err=%v), esperado 1 share", byMe, err)
	}
	withMe, err := env.svc.ListSharesWithMe(ctx, target)
	if err != nil || len(withMe) != 1 {
		t.Fatalf("ListSharesWithMe antes de revocar = %+v (err=%v), esperado 1 share", withMe, err)
	}

	if err := env.svc.RevokeShare(ctx, owner, share.ID); err != nil {
		t.Fatalf("RevokeShare falló: %v", err)
	}

	byMe, err = env.svc.ListSharesByMe(ctx, owner)
	if err != nil || len(byMe) != 0 {
		t.Errorf("ListSharesByMe tras revocar = %+v (err=%v), esperado 0 shares", byMe, err)
	}
	withMe, err = env.svc.ListSharesWithMe(ctx, target)
	if err != nil || len(withMe) != 0 {
		t.Errorf("ListSharesWithMe tras revocar = %+v (err=%v), esperado 0 shares", withMe, err)
	}
}
