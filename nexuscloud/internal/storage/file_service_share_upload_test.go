package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// Subida a una carpeta compartida con un usuario o un grupo (§37, ADR-035): el
// destinatario autenticado sube a la carpeta del propietario. Hasta ahora solo
// los enlaces públicos podían subir.

func (e *testEnv) removeFromGroup(t *testing.T, userID, groupID string) {
	t.Helper()
	if _, err := e.conn.ExecContext(context.Background(),
		`DELETE FROM user_groups WHERE user_id = ? AND group_id = ?`, userID, groupID); err != nil {
		t.Fatalf("quitando al usuario %s del grupo %s: %v", userID, groupID, err)
	}
}

func mkFolder(t *testing.T, env *testEnv, owner, parent, name string) *Directory {
	t.Helper()
	dir, err := env.svc.Mkdir(context.Background(), owner, parent, name, "")
	if err != nil {
		t.Fatalf("Mkdir(%s/%s) falló: %v", parent, name, err)
	}
	return dir
}

// grant comparte dir con un usuario (targetUser) o un grupo (targetGroup),
// con permiso de subida si upload es true.
func grant(t *testing.T, env *testEnv, owner string, dir *Directory, targetUser, targetGroup string, upload bool, maxBytes *int64) *Share {
	t.Helper()
	in := CreateShareInput{
		ResourceIsDirectory: true, ResourceID: dir.ID, CanDownload: true, CanUpload: upload, MaxUploadSizeBytes: maxBytes,
	}
	if targetGroup != "" {
		in.Type, in.TargetGroupID = ShareTypeGroup, targetGroup
	} else {
		in.Type, in.TargetUserID = ShareTypeUser, targetUser
	}
	share, _, err := env.svc.CreateShare(context.Background(), owner, in)
	if err != nil {
		t.Fatalf("CreateShare falló: %v", err)
	}
	return share
}

func sharedUpload(env *testEnv, requester string, dir *Directory, name, content string) (*FileMeta, string, error) {
	return env.svc.UploadToSharedDirectory(context.Background(), SharedUploadInput{
		RequesterID: requester, DirectoryID: dir.ID, Name: name, Content: strings.NewReader(content),
	})
}

func ptrInt64(v int64) *int64 { return &v }

func listNames(t *testing.T, env *testEnv, owner, folder string) []string {
	t.Helper()
	res, err := env.svc.List(context.Background(), owner, folder)
	if err != nil {
		t.Fatalf("List(%s) falló: %v", folder, err)
	}
	var out []string
	for _, f := range res.Files {
		out = append(out, f.Name)
	}
	return out
}

func TestUserAndGroupSharesCanCarryTheUploadPermission(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	groupID := env.group(t, "Equipo")
	dir := mkFolder(t, env, owner, "/", "Proyectos")

	if s := grant(t, env, owner, dir, target, "", true, nil); !s.CanUpload {
		t.Error("un share de usuario sobre una carpeta debe poder llevar la subida cuando se pide")
	}
	if s := grant(t, env, owner, dir, "", groupID, true, nil); !s.CanUpload {
		t.Error("un share de grupo sobre una carpeta debe poder llevar la subida cuando se pide")
	}
	if s := grant(t, env, owner, dir, target, "", false, nil); s.CanUpload {
		t.Error("sin pedirlo, un share de usuario sigue siendo de solo descarga")
	}

	// Lo pedido se guarda de verdad, no solo se devuelve.
	shares, err := env.svc.ListSharesByMe(context.Background(), owner)
	if err != nil {
		t.Fatalf("ListSharesByMe falló: %v", err)
	}
	var withUpload int
	for _, s := range shares {
		if s.CanUpload {
			withUpload++
		}
	}
	if withUpload != 2 {
		t.Errorf("shares con subida persistidos = %d, esperados 2", withUpload)
	}
}

func TestUploadPermissionOnUserAndGroupSharesNeedsAFolderAndDownload(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	groupID := env.group(t, "Equipo")
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	file, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: strings.NewReader("a")})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	for name, in := range map[string]CreateShareInput{
		"archivo con subida (usuario)":  {ResourceID: file.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: true, CanUpload: true},
		"archivo con subida (grupo)":    {ResourceID: file.ID, Type: ShareTypeGroup, TargetGroupID: groupID, CanDownload: true, CanUpload: true},
		"subida sin descarga (usuario)": {ResourceIsDirectory: true, ResourceID: dir.ID, Type: ShareTypeUser, TargetUserID: target, CanDownload: false, CanUpload: true},
		"subida sin descarga (grupo)":   {ResourceIsDirectory: true, ResourceID: dir.ID, Type: ShareTypeGroup, TargetGroupID: groupID, CanDownload: false, CanUpload: true},
	} {
		if _, _, err := env.svc.CreateShare(ctx, owner, in); !errors.Is(err, ErrInvalidShare) {
			t.Errorf("%s: err = %v, esperado ErrInvalidShare", name, err)
		}
	}
}

func TestSharedUploadStoresTheFileInTheOwnersTree(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	stranger := env.user(t, "stranger")
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	share := grant(t, env, owner, dir, target, "", true, nil)

	meta, shareID, err := sharedUpload(env, target, dir, "acta.txt", "contenido subido")
	if err != nil {
		t.Fatalf("la subida del destinatario falló: %v", err)
	}
	if meta.OwnerID != owner || meta.ParentPath != "/Proyectos" || meta.Name != "acta.txt" {
		t.Errorf("archivo = owner %q, carpeta %q, nombre %q; esperado el del propietario, /Proyectos, acta.txt", meta.OwnerID, meta.ParentPath, meta.Name)
	}
	if shareID != share.ID {
		t.Errorf("shareID devuelto = %q, esperado el share que autorizó la subida (%q)", shareID, share.ID)
	}
	if got := listNames(t, env, owner, "/Proyectos"); len(got) != 1 || got[0] != "acta.txt" {
		t.Errorf("el propietario ve %v, esperado [acta.txt]", got)
	}
	// El destinatario puede leerlo (el share es de lectura + subida)...
	if _, rc, err := env.svc.Download(ctx, target, meta.ID); err != nil {
		t.Errorf("el destinatario debería poder descargar lo que subió: %v", err)
	} else {
		b, _ := io.ReadAll(rc)
		rc.Close()
		if string(b) != "contenido subido" {
			t.Errorf("contenido = %q", b)
		}
	}
	// ...y un extraño no.
	if _, _, err := env.svc.Download(ctx, stranger, meta.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("descarga de un extraño: err = %v, esperado ErrForbidden", err)
	}
}

func TestSharedUploadValidatesTheFileName(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	grant(t, env, owner, dir, target, "", true, nil)

	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		if _, _, err := sharedUpload(env, target, dir, bad, "x"); !errors.Is(err, ErrInvalidName) {
			t.Errorf("nombre %q: err = %v, esperado ErrInvalidName", bad, err)
		}
	}
	if got := listNames(t, env, owner, "/Proyectos"); len(got) != 0 {
		t.Errorf("no debía crearse nada, hay %v", got)
	}
}

func TestSharedUploadIsDeniedWithoutAValidGrant(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	stranger := env.user(t, "stranger")
	outsider := env.user(t, "outsider") // en un grupo con permiso... del que no es miembro
	groupID := env.group(t, "Equipo")
	dir := mkFolder(t, env, owner, "/", "Proyectos")

	t.Run("un extraño sin ningún share", func(t *testing.T) {
		if _, _, err := sharedUpload(env, stranger, dir, "x.txt", "x"); !errors.Is(err, ErrForbidden) {
			t.Errorf("err = %v, esperado ErrForbidden", err)
		}
	})

	t.Run("un share de solo lectura distingue 'sin subida' de 'sin acceso'", func(t *testing.T) {
		env := newTestEnv(t, true)
		owner, target := env.user(t, "owner"), env.user(t, "target")
		dir := mkFolder(t, env, owner, "/", "Proyectos")
		grant(t, env, owner, dir, target, "", false, nil)
		if _, _, err := sharedUpload(env, target, dir, "x.txt", "x"); !errors.Is(err, ErrShareUploadNotAllowed) {
			t.Errorf("err = %v, esperado ErrShareUploadNotAllowed", err)
		}
	})

	t.Run("un share revocado no concede nada", func(t *testing.T) {
		share := grant(t, env, owner, dir, target, "", true, nil)
		if _, _, err := sharedUpload(env, target, dir, "antes.txt", "x"); err != nil {
			t.Fatalf("antes de revocar debía poder subir: %v", err)
		}
		if err := env.svc.RevokeShare(ctx, owner, share.ID); err != nil {
			t.Fatalf("RevokeShare falló: %v", err)
		}
		if _, _, err := sharedUpload(env, target, dir, "despues.txt", "x"); !errors.Is(err, ErrForbidden) {
			t.Errorf("tras revocar: err = %v, esperado ErrForbidden", err)
		}
	})

	t.Run("un share expirado no concede nada", func(t *testing.T) {
		env := newTestEnv(t, true)
		owner, target := env.user(t, "owner"), env.user(t, "target")
		dir := mkFolder(t, env, owner, "/", "Proyectos")
		// CreateShare exige una expiración futura: la fila caducada se crea directamente.
		past := time.Now().UTC().Add(-time.Hour)
		if err := env.shares.CreateShare(ctx, &Share{
			ID: "share-caducado", OwnerID: owner, DirectoryID: dir.ID, Type: ShareTypeUser, TargetUserID: target,
			CanDownload: true, CanUpload: true, ExpiresAt: &past, CreatedAt: past, UpdatedAt: past,
		}); err != nil {
			t.Fatalf("creando el share caducado: %v", err)
		}
		if _, _, err := sharedUpload(env, target, dir, "x.txt", "x"); !errors.Is(err, ErrForbidden) {
			t.Errorf("err = %v, esperado ErrForbidden", err)
		}
	})

	t.Run("un share de grupo no sirve a quien no es miembro", func(t *testing.T) {
		grant(t, env, owner, dir, "", groupID, true, nil)
		if _, _, err := sharedUpload(env, outsider, dir, "x.txt", "x"); !errors.Is(err, ErrForbidden) {
			t.Errorf("err = %v, esperado ErrForbidden", err)
		}
	})

	t.Run("con la compartición desactivada", func(t *testing.T) {
		off := newTestEnvFull(t, true, true, 10, 0, 0, false, true)
		o := off.user(t, "owner")
		d := mkFolder(t, off, o, "/", "Proyectos")
		if _, _, err := sharedUpload(off, o, d, "x.txt", "x"); !errors.Is(err, ErrSharingDisabled) {
			t.Errorf("err = %v, esperado ErrSharingDisabled", err)
		}
	})
}

func TestSharedUploadFollowsGroupMembership(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	member := env.user(t, "member")
	groupID := env.group(t, "Equipo")
	env.addToGroup(t, member, groupID)
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	grant(t, env, owner, dir, "", groupID, true, nil)

	if _, _, err := sharedUpload(env, member, dir, "uno.txt", "1"); err != nil {
		t.Fatalf("un miembro del grupo debe poder subir: %v", err)
	}
	env.removeFromGroup(t, member, groupID)
	if _, _, err := sharedUpload(env, member, dir, "dos.txt", "2"); !errors.Is(err, ErrForbidden) {
		t.Errorf("tras salir del grupo: err = %v, esperado ErrForbidden", err)
	}
}

func TestAnAncestorShareAllowsUploadingIntoItsSubfolders(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	parent := mkFolder(t, env, owner, "/", "Proyectos")
	child := mkFolder(t, env, owner, "/Proyectos", "2026")
	grant(t, env, owner, parent, target, "", true, nil)

	meta, _, err := sharedUpload(env, target, child, "acta.txt", "x")
	if err != nil {
		t.Fatalf("subir a una subcarpeta de una carpeta compartida debe estar permitido: %v", err)
	}
	if meta.ParentPath != "/Proyectos/2026" {
		t.Errorf("ParentPath = %q, esperado /Proyectos/2026", meta.ParentPath)
	}

	// El permiso baja por el árbol, no sube: un share solo sobre la subcarpeta
	// no permite subir a la carpeta padre.
	env2 := newTestEnv(t, true)
	o2, t2 := env2.user(t, "owner"), env2.user(t, "target")
	p2 := mkFolder(t, env2, o2, "/", "Proyectos")
	c2 := mkFolder(t, env2, o2, "/Proyectos", "2026")
	grant(t, env2, o2, c2, t2, "", true, nil)
	if _, _, err := sharedUpload(env2, t2, p2, "x.txt", "x"); !errors.Is(err, ErrForbidden) {
		t.Errorf("subir a la carpeta padre con un share solo de la subcarpeta: err = %v, esperado ErrForbidden", err)
	}
}

func TestSharedUploadNeverOverwritesAnExistingFile(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	grant(t, env, owner, dir, target, "", true, nil)
	original, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/Proyectos", Name: "informe.txt", Content: strings.NewReader("original")})
	if err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	if _, _, err := sharedUpload(env, target, dir, "informe.txt", "intento de pisarlo"); !errors.Is(err, ErrDestinationOccupied) {
		t.Fatalf("err = %v, esperado ErrDestinationOccupied", err)
	}
	_, rc, err := env.svc.Download(ctx, owner, original.ID)
	if err != nil {
		t.Fatalf("Download falló: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "original" {
		t.Errorf("contenido tras el intento = %q, debe seguir siendo %q", b, "original")
	}
	if versions, err := env.svc.ListVersions(ctx, owner, original.ID); err != nil || len(versions) != 0 {
		t.Errorf("versiones = %d (err %v): un intento rechazado no debe crear ninguna", len(versions), err)
	}

	// Un nombre que ocupa la papelera tampoco se reutiliza (§128).
	if err := env.svc.Delete(ctx, owner, original.ID); err != nil {
		t.Fatalf("Delete falló: %v", err)
	}
	if _, _, err := sharedUpload(env, target, dir, "informe.txt", "otra vez"); !errors.Is(err, ErrNameOccupiedByTrash) {
		t.Errorf("con el nombre en la papelera: err = %v, esperado ErrNameOccupiedByTrash", err)
	}
}

func TestSharedUploadHonoursTheSizeLimitOfTheMostPermissiveGrant(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	groupID := env.group(t, "Equipo")
	env.addToGroup(t, target, groupID)
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	grant(t, env, owner, dir, target, "", true, ptrInt64(5))

	if _, _, err := sharedUpload(env, target, dir, "justo.txt", "12345"); err != nil {
		t.Errorf("un archivo exactamente en el límite debe subir: %v", err)
	}
	if _, _, err := sharedUpload(env, target, dir, "grande.txt", "1234567890"); !errors.Is(err, ErrShareUploadTooLarge) {
		t.Errorf("err = %v, esperado ErrShareUploadTooLarge", err)
	}
	if got := listNames(t, env, owner, "/Proyectos"); len(got) != 1 || got[0] != "justo.txt" {
		t.Errorf("tras el rechazo hay %v; no debe quedar ningún parcial de grande.txt", got)
	}

	// Un segundo permiso aplicable con un límite mayor gana (unión de permisos).
	grant(t, env, owner, dir, "", groupID, true, ptrInt64(20))
	if _, _, err := sharedUpload(env, target, dir, "medio.txt", "1234567890"); err != nil {
		t.Errorf("con un permiso de grupo de 20 bytes, 10 deben caber: %v", err)
	}
	if _, _, err := sharedUpload(env, target, dir, "enorme.txt", strings.Repeat("x", 30)); !errors.Is(err, ErrShareUploadTooLarge) {
		t.Errorf("30 bytes con el mayor límite en 20: err = %v, esperado ErrShareUploadTooLarge", err)
	}

	// Y uno sin límite gana sobre cualquier otro.
	grant(t, env, owner, dir, target, "", true, nil)
	if _, _, err := sharedUpload(env, target, dir, "sinlimite.txt", strings.Repeat("x", 30)); err != nil {
		t.Errorf("con un permiso sin límite, 30 bytes deben caber: %v", err)
	}
}

func TestSharedUploadRefusesAFolderInTheTrash(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	target := env.user(t, "target")
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	grant(t, env, owner, dir, target, "", true, nil)
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}
	if _, _, err := sharedUpload(env, target, dir, "x.txt", "x"); !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("err = %v, esperado ErrDirectoryNotFound", err)
	}
}

// SharedUploadPermission es lo que la interfaz consulta para saber si ofrecer
// el botón de subir y qué límite de tamaño anunciar: el mismo resultado que
// luego aplicaría UploadToSharedDirectory.
func TestSharedUploadPermissionReportsTheGrantAndItsLimit(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	uploader := env.user(t, "uploader")
	limited := env.user(t, "limited")
	teamMate := env.user(t, "teammate")
	reader := env.user(t, "reader")
	stranger := env.user(t, "stranger")
	groupID := env.group(t, "Equipo")
	env.addToGroup(t, teamMate, groupID)
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	grant(t, env, owner, dir, uploader, "", true, nil)
	grant(t, env, owner, dir, limited, "", true, ptrInt64(100))
	grant(t, env, owner, dir, reader, "", false, nil)
	grant(t, env, owner, dir, teamMate, "", true, ptrInt64(100))
	grant(t, env, owner, dir, "", groupID, true, ptrInt64(500))

	sameLimit := func(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	for name, tc := range map[string]struct {
		user    string
		allowed bool
		max     *int64
	}{
		"con permiso de subida sin límite":       {uploader, true, nil},
		"con un límite de tamaño":                {limited, true, ptrInt64(100)},
		"con dos permisos gana el mayor límite":  {teamMate, true, ptrInt64(500)},
		"de solo lectura":                        {reader, false, nil},
		"un extraño":                             {stranger, false, nil},
		"el propietario de la carpeta, sin tope": {owner, true, nil},
	} {
		got, err := env.svc.SharedUploadPermission(ctx, tc.user, dir.ID)
		if err != nil {
			t.Fatalf("%s: SharedUploadPermission falló: %v", name, err)
		}
		if got.Allowed != tc.allowed || !sameLimit(got.MaxSizeBytes, tc.max) {
			t.Errorf("%s: permiso = %+v, esperado Allowed=%v y límite %v", name, got, tc.allowed, tc.max)
		}
	}

	// Una carpeta en la papelera no admite subidas, ni siquiera de quien tenía permiso.
	if err := env.svc.DeleteDirectory(ctx, owner, dir.ID); err != nil {
		t.Fatalf("DeleteDirectory falló: %v", err)
	}
	if got, err := env.svc.SharedUploadPermission(ctx, uploader, dir.ID); err != nil || got.Allowed {
		t.Errorf("carpeta en la papelera: permiso = %+v, err = %v; esperado no permitido", got, err)
	}
}

func TestTheOwnerCanUseTheSharedUploadPathWithoutAShare(t *testing.T) {
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")
	dir := mkFolder(t, env, owner, "/", "Proyectos")
	meta, shareID, err := sharedUpload(env, owner, dir, "mio.txt", "x")
	if err != nil {
		t.Fatalf("el propietario debe poder subir a su propia carpeta: %v", err)
	}
	if shareID != "" || meta.OwnerID != owner {
		t.Errorf("shareID = %q, owner = %q; esperado sin share y el propio propietario", shareID, meta.OwnerID)
	}
}

// NoOverwrite es la pieza de Upload en la que se apoya la subida a compartidos:
// solo rechaza un archivo ACTIVO con ese nombre y no cambia el comportamiento
// normal (sobrescribir dejando una versión).
func TestUploadNoOverwriteOnlyRefusesAnExistingActiveFile(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t, true)
	owner := env.user(t, "owner")

	first, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("uno")), NoOverwrite: true})
	if err != nil {
		t.Fatalf("un nombre nuevo debe subir con NoOverwrite: %v", err)
	}
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("dos")), NoOverwrite: true}); !errors.Is(err, ErrDestinationOccupied) {
		t.Fatalf("err = %v, esperado ErrDestinationOccupied", err)
	}
	// Sin NoOverwrite, el comportamiento de siempre: sobrescribe y archiva la versión.
	if _, err := env.svc.Upload(ctx, UploadInput{OwnerID: owner, ParentPath: "/", Name: "a.txt", Content: bytes.NewReader([]byte("tres"))}); err != nil {
		t.Fatalf("sobrescribir sin NoOverwrite debe seguir funcionando: %v", err)
	}
	if versions, err := env.svc.ListVersions(ctx, owner, first.ID); err != nil || len(versions) != 1 {
		t.Errorf("versiones = %d (err %v), esperada 1 (el contenido anterior)", len(versions), err)
	}
}
