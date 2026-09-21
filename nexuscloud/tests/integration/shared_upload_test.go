// Tests de integración de la subida a una carpeta compartida con un usuario o
// un grupo (§37, ADR-035): POST /shared-directories/{id}/files y el campo
// can_upload de GET /shared-directories/{id}, por HTTP real sobre el servidor
// completo (router, middleware, handlers, servicios y repositorios).
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
)

type shareDTO struct {
	ID          string `json:"id"`
	CanDownload bool   `json:"can_download"`
	CanUpload   bool   `json:"can_upload"`
}

// sharedListingDTO refleja GET /shared-directories/{id} (y, sin los campos de
// subida, GET /files): subcarpetas, archivos, si quien mira puede subir y con
// qué límite por archivo (ausente = sin límite).
type sharedListingDTO struct {
	CanUpload          bool           `json:"can_upload"`
	MaxUploadSizeBytes *int64         `json:"max_upload_size_bytes"`
	Directories        []directoryDTO `json:"directories"`
	Files              []fileDTO      `json:"files"`
}

type errorDTO struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// sharedUploadEnv monta el escenario común: la dueña (super_admin, dueña de
// /Entregas), un miembro con quien se comparte y un ajeno sin ningún acceso.
type sharedUploadEnv struct {
	t        *testing.T
	c        *apiClient
	srv      *server.Server
	owner    string // tokens de sesión
	member   string
	stranger string
	memberID string
	dir      directoryDTO // /Entregas, de la dueña
}

func newSharedUploadEnv(t *testing.T) *sharedUploadEnv {
	t.Helper()
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	owner := loginToken(t, c, "duena", "contrasena-larga-123")
	memberID, member := createMemberUser(t, srv, c, owner, "miembro", "contrasena-del-miembro")
	_, stranger := createMemberUser(t, srv, c, owner, "ajeno", "contrasena-del-ajeno")

	e := &sharedUploadEnv{t: t, c: c, srv: srv, owner: owner, member: member, stranger: stranger, memberID: memberID}
	e.dir = e.mkdir("/", "Entregas")
	return e
}

// mkdir crea una carpeta de la dueña.
func (e *sharedUploadEnv) mkdir(parent, name string) directoryDTO {
	e.t.Helper()
	resp := e.c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": parent, "name": name}, e.owner)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("creando %s/%s: status = %d, body = %s", parent, name, resp.StatusCode, body)
	}
	return decodeJSON[directoryDTO](e.t, resp)
}

// share crea un share como la dueña y exige 201.
func (e *sharedUploadEnv) share(body map[string]any) shareDTO {
	e.t.Helper()
	resp := e.c.do(http.MethodPost, "/api/v1/shares", body, e.owner)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("crear share %v: status = %d, body = %s", body, resp.StatusCode, b)
	}
	return decodeJSON[shareDTO](e.t, resp)
}

// shareWithMember comparte una carpeta con el miembro (por usuario); extra
// añade o pisa campos del cuerpo (can_upload, max_upload_size_bytes...).
func (e *sharedUploadEnv) shareWithMember(dirID string, extra map[string]any) shareDTO {
	e.t.Helper()
	body := map[string]any{"resource_type": "directory", "resource_id": dirID, "share_type": "user", "target_username": "miembro"}
	for k, v := range extra {
		body[k] = v
	}
	return e.share(body)
}

// upload sube en crudo a POST /shared-directories/{id}/files y devuelve la
// respuesta sin consumir. Un token vacío hace la petición sin autenticar.
func (e *sharedUploadEnv) upload(token, dirID, name, content string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		e.c.base+"/api/v1/shared-directories/"+dirID+"/files?name="+url.QueryEscape(name), strings.NewReader(content))
	if err != nil {
		e.t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.c.client.Do(req)
	if err != nil {
		e.t.Fatalf("subiendo %s: %v", name, err)
	}
	return resp
}

func (e *sharedUploadEnv) listing(token, path string) sharedListingDTO {
	e.t.Helper()
	resp := e.c.do(http.MethodGet, path, nil, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("GET %s: status = %d, body = %s", path, resp.StatusCode, body)
	}
	return decodeJSON[sharedListingDTO](e.t, resp)
}

// ownerFileNames devuelve los archivos que la dueña ve en /Entregas.
func (e *sharedUploadEnv) ownerFileNames() []string {
	e.t.Helper()
	var names []string
	for _, f := range e.listing(e.owner, "/api/v1/files?path=/Entregas").Files {
		names = append(names, f.Name)
	}
	return names
}

func (e *sharedUploadEnv) download(token, fileID string) (int, string) {
	e.t.Helper()
	resp := e.c.do(http.MethodGet, "/api/v1/files/"+fileID, nil, token)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// wantErrorCode exige el status y, si code no es "", el código de error del
// cuerpo ({"error":{"code":...}}).
func wantErrorCode(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("status = %d (body %s), esperado %d %s", resp.StatusCode, body, status, code)
	}
	var e errorDTO
	_ = json.Unmarshal(body, &e)
	if code != "" && e.Error.Code != code {
		t.Errorf("código de error = %q (body %s), esperado %q", e.Error.Code, body, code)
	}
}

func TestSharedUploadStoresTheFileInTheOwnersFolder(t *testing.T) {
	e := newSharedUploadEnv(t)
	share := e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})
	if !share.CanUpload || !share.CanDownload {
		t.Fatalf("el share debía quedar con lectura y subida: %+v", share)
	}
	if l := e.listing(e.member, "/api/v1/shared-directories/"+e.dir.ID); !l.CanUpload || l.MaxUploadSizeBytes != nil {
		t.Errorf("GET /shared-directories/{id}: can_upload=%v max=%v, esperado true y sin límite", l.CanUpload, l.MaxUploadSizeBytes)
	}

	resp := e.upload(e.member, e.dir.ID, "informe.txt", "contenido del miembro")
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("subida del miembro: status = %d, body = %s", resp.StatusCode, body)
	}
	created := decodeJSON[fileDTO](t, resp)
	if created.Name != "informe.txt" || created.ParentPath != "/Entregas" {
		t.Errorf("archivo creado = %+v, esperado informe.txt en /Entregas", created)
	}

	// Vive en el árbol de la dueña y ambos pueden leerlo.
	if names := e.ownerFileNames(); len(names) != 1 || names[0] != "informe.txt" {
		t.Errorf("la dueña ve %v en /Entregas, esperado [informe.txt]", names)
	}
	for who, token := range map[string]string{"la dueña": e.owner, "el miembro": e.member} {
		if status, body := e.download(token, created.ID); status != http.StatusOK || body != "contenido del miembro" {
			t.Errorf("descarga de %s: status = %d, body = %q", who, status, body)
		}
	}
	if got := e.listing(e.member, "/api/v1/shared-directories/"+e.dir.ID).Files; len(got) != 1 || got[0].ID != created.ID {
		t.Errorf("el miembro debería ver el archivo en la carpeta compartida: %+v", got)
	}

	// Quién subió queda en la auditoría: actor = el miembro, no la dueña.
	events, err := audit.NewSQLRepository(db.Wrap("sqlite", e.srv.DB)).ListEvents(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	var upload *audit.Event
	for _, ev := range events {
		if ev.EventType == audit.EventUpload && ev.TargetID == created.ID {
			upload = ev
		}
	}
	if upload == nil {
		t.Fatal("falta el evento de auditoría 'upload' de la subida compartida")
	}
	if upload.ActorUserID != e.memberID {
		t.Errorf("actor del evento = %q, esperado el miembro %q", upload.ActorUserID, e.memberID)
	}
	if upload.Metadata["via"] != "shared_directory" || upload.Metadata["share_id"] != share.ID {
		t.Errorf("metadatos del evento = %v, esperado via=shared_directory y share_id=%s", upload.Metadata, share.ID)
	}
}

func TestSharedUploadIsDeniedWithoutTheUploadPermission(t *testing.T) {
	e := newSharedUploadEnv(t)
	e.shareWithMember(e.dir.ID, nil) // solo lectura

	if e.listing(e.member, "/api/v1/shared-directories/"+e.dir.ID).CanUpload {
		t.Error("un share de solo lectura no debería indicar can_upload=true")
	}
	wantErrorCode(t, e.upload(e.member, e.dir.ID, "a.txt", "x"), http.StatusForbidden, "upload_not_allowed")
	wantErrorCode(t, e.upload(e.stranger, e.dir.ID, "a.txt", "x"), http.StatusForbidden, "forbidden")
	wantErrorCode(t, e.upload("", e.dir.ID, "a.txt", "x"), http.StatusUnauthorized, "")
	wantErrorCode(t, e.upload(e.member, "no-existe", "a.txt", "x"), http.StatusNotFound, "not_found")

	if names := e.ownerFileNames(); len(names) != 0 {
		t.Errorf("ninguna subida denegada debía dejar archivos, pero hay %v", names)
	}
}

func TestSharedUploadNeverOverwritesAnExistingFile(t *testing.T) {
	e := newSharedUploadEnv(t)
	e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})
	original := uploadTestFile(t, e.c, e.owner, "/Entregas", "acta.txt", "original de la dueña")

	wantErrorCode(t, e.upload(e.member, e.dir.ID, "acta.txt", "intento de pisarlo"), http.StatusConflict, "destination_occupied")

	if status, body := e.download(e.owner, original.ID); status != http.StatusOK || body != "original de la dueña" {
		t.Errorf("el archivo original cambió: status = %d, body = %q", status, body)
	}
}

// Un nombre ocupado por algo de la papelera del propietario da el mismo 409 que
// uno ocupado por un archivo activo: quien sube no debe averiguar qué hay en esa
// papelera, ni el texto del error le invita a «restaurarlo».
func TestSharedUploadDoesNotRevealTheOwnersTrash(t *testing.T) {
	e := newSharedUploadEnv(t)
	e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})
	borrado := uploadTestFile(t, e.c, e.owner, "/Entregas", "borrado.txt", "algo que la dueña borró")
	if resp := e.c.do(http.MethodDelete, "/api/v1/files/"+borrado.ID, nil, e.owner); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("borrar el archivo = %d", resp.StatusCode)
	}

	resp := e.upload(e.member, e.dir.ID, "borrado.txt", "intento")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"destination_occupied"`) ||
		strings.Contains(strings.ToLower(string(body)), "papelera") {
		t.Errorf("status = %d, cuerpo = %s; esperado 409 destination_occupied sin mencionar la papelera", resp.StatusCode, body)
	}
}

func TestSharedUploadHonoursTheSizeLimitOfTheShare(t *testing.T) {
	e := newSharedUploadEnv(t)
	e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true, "max_upload_size_bytes": 10})

	// El límite se anuncia en el listado para que la interfaz no mande un
	// archivo que el servidor va a rechazar a medio subir.
	if got := e.listing(e.member, "/api/v1/shared-directories/"+e.dir.ID).MaxUploadSizeBytes; got == nil || *got != 10 {
		t.Errorf("max_upload_size_bytes del listado = %v, esperado 10", got)
	}
	wantErrorCode(t, e.upload(e.member, e.dir.ID, "grande.bin", strings.Repeat("x", 11)), http.StatusRequestEntityTooLarge, "upload_too_large")
	// Un cuerpo muy por encima del límite también recibe el 413 (el servidor
	// corta la lectura sin haberlo consumido entero y aun así responde).
	wantErrorCode(t, e.upload(e.member, e.dir.ID, "enorme.bin", strings.Repeat("x", 512<<10)), http.StatusRequestEntityTooLarge, "upload_too_large")
	resp := e.upload(e.member, e.dir.ID, "justo.bin", strings.Repeat("x", 10))
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("un archivo de exactamente el límite debía subir: status = %d, body = %s", resp.StatusCode, body)
	}
	if names := e.ownerFileNames(); len(names) != 1 || names[0] != "justo.bin" {
		t.Errorf("la dueña ve %v, esperado solo [justo.bin] (el grande no debe dejar nada)", names)
	}
}

func TestSharedUploadThroughAGroupShare(t *testing.T) {
	e := newSharedUploadEnv(t)
	resp := e.c.do(http.MethodPost, "/api/v1/groups", map[string]string{"name": "Equipo"}, e.owner)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear grupo = %d", resp.StatusCode)
	}
	group := decodeJSON[struct {
		ID string `json:"id"`
	}](t, resp)
	if resp = e.c.do(http.MethodPost, "/api/v1/groups/"+group.ID+"/members", map[string]string{"user_id": e.memberID}, e.owner); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("añadir miembro = %d", resp.StatusCode)
	}
	e.share(map[string]any{
		"resource_type": "directory", "resource_id": e.dir.ID, "share_type": "group", "target_group_id": group.ID, "can_upload": true,
	})

	if resp := e.upload(e.member, e.dir.ID, "del-equipo.txt", "x"); resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("subida de un miembro del grupo: status = %d, body = %s", resp.StatusCode, body)
	}
	wantErrorCode(t, e.upload(e.stranger, e.dir.ID, "intruso.txt", "x"), http.StatusForbidden, "forbidden")
}

func TestSharedUploadIntoASubfolderUsesTheAncestorShare(t *testing.T) {
	e := newSharedUploadEnv(t)
	sub := e.mkdir("/Entregas", "Parcial")
	e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})

	if !e.listing(e.member, "/api/v1/shared-directories/"+sub.ID).CanUpload {
		t.Error("la subcarpeta de una carpeta compartida con subida debería indicar can_upload=true")
	}
	resp := e.upload(e.member, sub.ID, "p1.txt", "x")
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("subida a la subcarpeta: status = %d, body = %s", resp.StatusCode, body)
	}
	if created := decodeJSON[fileDTO](t, resp); created.ParentPath != "/Entregas/Parcial" {
		t.Errorf("parent_path = %q, esperado /Entregas/Parcial", created.ParentPath)
	}
}

func TestSharedUploadStopsWhenTheShareIsRevoked(t *testing.T) {
	e := newSharedUploadEnv(t)
	share := e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})
	if resp := e.upload(e.member, e.dir.ID, "antes.txt", "x"); resp.StatusCode != http.StatusCreated {
		t.Fatalf("antes de revocar debía poder subir: status = %d", resp.StatusCode)
	}

	if resp := e.c.do(http.MethodDelete, "/api/v1/shares/"+share.ID, nil, e.owner); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revocar = %d", resp.StatusCode)
	}
	wantErrorCode(t, e.upload(e.member, e.dir.ID, "despues.txt", "x"), http.StatusForbidden, "forbidden")
}

func TestSharedUploadValidatesTheFileName(t *testing.T) {
	e := newSharedUploadEnv(t)
	e.shareWithMember(e.dir.ID, map[string]any{"can_upload": true})

	wantErrorCode(t, e.upload(e.member, e.dir.ID, "../fuera.txt", "x"), http.StatusBadRequest, "invalid_request")
	wantErrorCode(t, e.upload(e.member, e.dir.ID, "", "x"), http.StatusBadRequest, "invalid_request")
	if names := e.ownerFileNames(); len(names) != 0 {
		t.Errorf("un nombre inválido no debía dejar archivos: %v", names)
	}
}

func TestCreateShareRejectsUploadPermissionWithoutReadOrOnAFile(t *testing.T) {
	e := newSharedUploadEnv(t)
	file := uploadTestFile(t, e.c, e.owner, "/", "suelto.txt", "x")

	for name, body := range map[string]map[string]any{
		"un archivo no puede llevar permiso de subida": {
			"resource_type": "file", "resource_id": file.ID, "share_type": "user", "target_username": "miembro", "can_upload": true,
		},
		"la subida a un usuario exige poder leer": {
			"resource_type": "directory", "resource_id": e.dir.ID, "share_type": "user", "target_username": "miembro",
			"can_upload": true, "can_download": false,
		},
	} {
		resp := e.c.do(http.MethodPost, "/api/v1/shares", body, e.owner)
		if resp.StatusCode != http.StatusBadRequest {
			b, _ := io.ReadAll(resp.Body)
			t.Errorf("%s: status = %d, body = %s, esperado 400", name, resp.StatusCode, b)
		}
	}
}
