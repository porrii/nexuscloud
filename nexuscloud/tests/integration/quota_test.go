// Tests de integración de las cuotas de almacenamiento (§24, ADR-036) por HTTP
// real sobre el servidor completo: 507 al pasarse, GET /users/me/quota, fijar
// la cuota de usuarios y grupos (PATCH tri-estado), la cuota global, y que los
// archivos subidos a carpetas ajenas (compartidas o por enlace) cuentan contra
// el propietario.
package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/audit"
	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/server"
)

// newQuotaTestServer es newTestServer con los enlaces públicos activados y,
// opcionalmente, una cuota global (0 = sin ella).
func newQuotaTestServer(t *testing.T, defaultQuota int64) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Storage.DefaultQuotaBytes = defaultQuota
	cfg.Database.Driver = "sqlite"
	cfg.Security.RateLimit.LoginPerMinute = 1000
	cfg.Security.RateLimit.APIPerMinute = 10000
	cfg.API.Enabled = true
	cfg.Sharing.PublicLinksEnabled = true

	srv, err := server.Build(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("server.Build falló: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

// quotaDTO refleja GET /users/me/quota.
type quotaDTO struct {
	UsedBytes     int64  `json:"used_bytes"`
	FilesBytes    int64  `json:"files_bytes"`
	TrashBytes    int64  `json:"trash_bytes"`
	VersionsBytes int64  `json:"versions_bytes"`
	LimitBytes    *int64 `json:"limit_bytes"`
	Source        string `json:"source"`
	GroupName     string `json:"group_name"`
}

type userQuotaDTO struct {
	ID         string `json:"id"`
	QuotaBytes *int64 `json:"quota_bytes"`
}

type groupQuotaDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	QuotaBytes *int64 `json:"quota_bytes"`
}

// quotaEnv monta el escenario común: la administradora (super_admin, dueña
// de sus archivos) y un miembro sin cuota alguna.
type quotaEnv struct {
	t        *testing.T
	c        *apiClient
	srv      *server.Server
	admin    string // token de sesión
	adminID  string
	member   string
	memberID string
}

func newQuotaEnv(t *testing.T, defaultQuota int64) *quotaEnv {
	t.Helper()
	ts, srv := newQuotaTestServer(t, defaultQuota)
	createAdmin(t, srv, "duena", "contrasena-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	admin := loginToken(t, c, "duena", "contrasena-larga-123")
	memberID, member := createMemberUser(t, srv, c, admin, "miembro", "contrasena-del-miembro")
	me := decodeJSON[userQuotaDTO](t, c.do(http.MethodGet, "/api/v1/users/me", nil, admin))
	return &quotaEnv{t: t, c: c, srv: srv, admin: admin, adminID: me.ID, member: member, memberID: memberID}
}

// setUserQuota hace PATCH /users/{id} con quota_bytes = quota (un número o nil = hereda).
func (e *quotaEnv) setUserQuota(userID string, quota any) *http.Response {
	e.t.Helper()
	return e.c.do(http.MethodPatch, "/api/v1/users/"+userID, map[string]any{"quota_bytes": quota}, e.admin)
}

func (e *quotaEnv) mustSetUserQuota(userID string, quota any) {
	e.t.Helper()
	resp := e.setUserQuota(userID, quota)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("PATCH quota_bytes=%v: status = %d, cuerpo = %s", quota, resp.StatusCode, b)
	}
	resp.Body.Close()
}

// upload sube en crudo a POST /files (raíz) y devuelve la respuesta sin consumir.
func (e *quotaEnv) upload(token, name, content string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		e.c.base+"/api/v1/files?name="+url.QueryEscape(name)+"&path=/", strings.NewReader(content))
	if err != nil {
		e.t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := e.c.client.Do(req)
	if err != nil {
		e.t.Fatalf("subiendo %s: %v", name, err)
	}
	return resp
}

func (e *quotaEnv) mustUpload(token, name, content string) {
	e.t.Helper()
	resp := e.upload(token, name, content)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("subir %s: status = %d, cuerpo = %s", name, resp.StatusCode, b)
	}
}

func (e *quotaEnv) quota(token string) quotaDTO {
	e.t.Helper()
	resp := e.c.do(http.MethodGet, "/api/v1/users/me/quota", nil, token)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("GET /users/me/quota: status = %d, cuerpo = %s", resp.StatusCode, b)
	}
	return decodeJSON[quotaDTO](e.t, resp)
}

func TestQuotaUploadIsRejectedWith507AndTheUsageIsReported(t *testing.T) {
	e := newQuotaEnv(t, 0)

	// Sin cuota configurada en ningún nivel: sin límite, y el uso se informa igual.
	if q := e.quota(e.member); q.LimitBytes != nil || q.Source != "none" || q.UsedBytes != 0 {
		t.Fatalf("cuota inicial = %+v, esperado sin límite, origen none y uso 0", q)
	}

	e.mustSetUserQuota(e.memberID, 100)
	e.mustUpload(e.member, "a.bin", strings.Repeat("a", 60))

	q := e.quota(e.member)
	if q.UsedBytes != 60 || q.FilesBytes != 60 || q.LimitBytes == nil || *q.LimitBytes != 100 || q.Source != "user" {
		t.Errorf("cuota tras subir 60 = %+v, esperado uso 60 de 100, origen user", q)
	}

	wantErrorCode(t, e.upload(e.member, "b.bin", strings.Repeat("b", 50)), http.StatusInsufficientStorage, "quota_exceeded")
	if q := e.quota(e.member); q.UsedBytes != 60 {
		t.Errorf("uso tras el rechazo = %d, esperado 60: lo rechazado no debe contar", q.UsedBytes)
	}

	// Ampliar la cuota lo desbloquea; 0 = ilimitada.
	e.mustSetUserQuota(e.memberID, 200)
	e.mustUpload(e.member, "b.bin", strings.Repeat("b", 50))
	e.mustSetUserQuota(e.memberID, 0)
	e.mustUpload(e.member, "c.bin", strings.Repeat("c", 5000))
	if q := e.quota(e.member); q.LimitBytes != nil || q.Source != "user" || q.UsedBytes != 5110 {
		t.Errorf("con ilimitada explícita = %+v, esperado sin límite, origen user y uso 5110", q)
	}
}

func TestQuotaPatchIsTriStateAndValidated(t *testing.T) {
	e := newQuotaEnv(t, 0)

	patch := func(body map[string]any) (int, userQuotaDTO) {
		resp := e.c.do(http.MethodPatch, "/api/v1/users/"+e.memberID, body, e.admin)
		defer resp.Body.Close()
		var u userQuotaDTO
		if resp.StatusCode == http.StatusOK {
			u = decodeJSON[userQuotaDTO](t, resp)
		}
		return resp.StatusCode, u
	}

	if status, u := patch(map[string]any{"quota_bytes": 107374182400}); status != http.StatusOK || u.QuotaBytes == nil || *u.QuotaBytes != 107374182400 {
		t.Fatalf("PATCH 100 GiB: status = %d, quota_bytes = %v", status, u.QuotaBytes)
	}
	// Un PATCH que no menciona la cuota no la toca.
	if status, u := patch(map[string]any{"display_name": "Otra persona"}); status != http.StatusOK || u.QuotaBytes == nil || *u.QuotaBytes != 107374182400 {
		t.Errorf("PATCH sin quota_bytes: status = %d, quota_bytes = %v, esperado que se conserve", status, u.QuotaBytes)
	}
	if status, u := patch(map[string]any{"quota_bytes": 0}); status != http.StatusOK || u.QuotaBytes == nil || *u.QuotaBytes != 0 {
		t.Errorf("PATCH 0 (ilimitada): status = %d, quota_bytes = %v, esperado 0", status, u.QuotaBytes)
	}
	if status, u := patch(map[string]any{"quota_bytes": nil}); status != http.StatusOK || u.QuotaBytes != nil {
		t.Errorf("PATCH null (hereda): status = %d, quota_bytes = %v, esperado ausente", status, u.QuotaBytes)
	}

	for name, body := range map[string]map[string]any{
		"negativa":   {"quota_bytes": -1},
		"texto":      {"quota_bytes": "100GB"},
		"decimal":    {"quota_bytes": 1.5},
		"desbordada": {"quota_bytes": 1e30},
	} {
		if status, _ := patch(body); status != http.StatusBadRequest {
			t.Errorf("PATCH con cuota %s: status = %d, esperado 400", name, status)
		}
	}
}

func TestQuotaCanOnlyBeSetByAnAdmin(t *testing.T) {
	e := newQuotaEnv(t, 0)

	resp := e.c.do(http.MethodPatch, "/api/v1/users/"+e.memberID, map[string]any{"quota_bytes": 0}, e.member)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("un usuario normal cambiándose la cuota: status = %d, esperado 403", resp.StatusCode)
	}
}

func TestMyQuotaRequiresAuthentication(t *testing.T) {
	e := newQuotaEnv(t, 0)
	resp := e.c.do(http.MethodGet, "/api/v1/users/me/quota", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("sin sesión: status = %d, esperado 401", resp.StatusCode)
	}
}

func TestCreateUserAcceptsAQuota(t *testing.T) {
	e := newQuotaEnv(t, 0)

	resp := e.c.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "conlimite", "password": "contrasena-larga-123", "quota_bytes": 5368709120,
	}, e.admin)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /users con cuota: status = %d, cuerpo = %s", resp.StatusCode, b)
	}
	if u := decodeJSON[userQuotaDTO](t, resp); u.QuotaBytes == nil || *u.QuotaBytes != 5368709120 {
		t.Errorf("quota_bytes = %v, esperado 5 GiB", u.QuotaBytes)
	}

	resp = e.c.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "negativo", "password": "contrasena-larga-123", "quota_bytes": -5,
	}, e.admin)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST /users con cuota negativa: status = %d, esperado 400", resp.StatusCode)
	}
}

// El desglose explica en qué se va el espacio: borrar a la papelera no lo libera
// y sobrescribir deja la versión anterior.
func TestQuotaBreakdownShowsTrashAndVersions(t *testing.T) {
	e := newQuotaEnv(t, 0)
	e.mustSetUserQuota(e.memberID, 1000)

	e.mustUpload(e.member, "a.bin", strings.Repeat("a", 40))
	e.mustUpload(e.member, "b.bin", strings.Repeat("b", 30))
	e.mustUpload(e.member, "b.bin", strings.Repeat("B", 20)) // deja la versión de 30

	list := decodeJSON[struct {
		Files []fileDTO `json:"files"`
	}](t, e.c.do(http.MethodGet, "/api/v1/files?path=/", nil, e.member))
	for _, f := range list.Files {
		if f.Name == "a.bin" {
			resp := e.c.do(http.MethodDelete, "/api/v1/files/"+f.ID, nil, e.member)
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("borrar a.bin = %d", resp.StatusCode)
			}
		}
	}

	q := e.quota(e.member)
	if q.FilesBytes != 20 || q.TrashBytes != 40 || q.VersionsBytes != 30 || q.UsedBytes != 90 {
		t.Errorf("desglose = %+v, esperado 20 activos, 40 en papelera, 30 en versiones y 90 en total", q)
	}
}

// Los archivos de una carpeta compartida pertenecen al propietario (ADR-035):
// cuentan contra SU cuota, y a quien sube no se le revela cuánto usa.
func TestQuotaOfASharedFolderIsTheOwnersAndTheMessageIsGeneric(t *testing.T) {
	e := newSharedUploadEnv(t)
	me := decodeJSON[userQuotaDTO](t, e.c.do(http.MethodGet, "/api/v1/users/me", nil, e.owner))
	patch := func(userID string, quota int64) {
		resp := e.c.do(http.MethodPatch, "/api/v1/users/"+userID, map[string]any{"quota_bytes": quota}, e.owner)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PATCH cuota: status = %d", resp.StatusCode)
		}
	}
	patch(me.ID, 100)
	patch(e.memberID, 10) // la del que sube no debe importar
	e.shareWithMember(e.dir.ID, map[string]any{"can_download": true, "can_upload": true})

	up := e.upload(e.member, e.dir.ID, "a.txt", strings.Repeat("x", 60))
	up.Body.Close()
	if up.StatusCode != http.StatusCreated {
		t.Fatalf("60 bytes caben en la cuota de la propietaria (100): status = %d", up.StatusCode)
	}

	resp := e.upload(e.member, e.dir.ID, "b.txt", strings.Repeat("y", 50))
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInsufficientStorage || !strings.Contains(string(body), `"quota_exceeded"`) {
		t.Fatalf("status = %d, cuerpo = %s; esperado 507 quota_exceeded", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "No hay espacio disponible en esta carpeta.") ||
		strings.Contains(string(body), "100") || strings.Contains(string(body), "60") {
		t.Errorf("el mensaje debe ser genérico y no revelar el uso ni la cuota de la propietaria: %s", body)
	}

	quotaOf := func(token string) quotaDTO {
		return decodeJSON[quotaDTO](t, e.c.do(http.MethodGet, "/api/v1/users/me/quota", nil, token))
	}
	if q := quotaOf(e.owner); q.UsedBytes != 60 {
		t.Errorf("uso de la propietaria = %d, esperado 60", q.UsedBytes)
	}
	if q := quotaOf(e.member); q.UsedBytes != 0 {
		t.Errorf("uso del que sube = %d, esperado 0: lo subido no es suyo", q.UsedBytes)
	}
}

func TestQuotaAppliesToPublicLinkUploads(t *testing.T) {
	e := newQuotaEnv(t, 0)
	e.mustSetUserQuota(e.adminID, 100)

	resp := e.c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Buzon"}, e.admin)
	dir := decodeJSON[directoryDTO](t, resp)
	resp = e.c.do(http.MethodPost, "/api/v1/shares", map[string]any{
		"resource_type": "directory", "resource_id": dir.ID, "share_type": "link", "can_upload": true,
	}, e.admin)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("crear el enlace = %d, cuerpo = %s", resp.StatusCode, b)
	}
	token := decodeJSON[tokenDTO](t, resp).Token

	upload := func(name, content string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost,
			e.c.base+"/api/v1/public/shares/"+token+"/upload?name="+url.QueryEscape(name), strings.NewReader(content))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := e.c.client.Do(req) // sin sesión: es un enlace público
		if err != nil {
			t.Fatalf("subiendo %s: %v", name, err)
		}
		return resp
	}

	fresh := upload("a.txt", strings.Repeat("x", 60))
	fresh.Body.Close()
	if fresh.StatusCode != http.StatusCreated {
		t.Fatalf("60 bytes caben: status = %d", fresh.StatusCode)
	}
	over := upload("b.txt", strings.Repeat("y", 50))
	defer over.Body.Close()
	body, _ := io.ReadAll(over.Body)
	if over.StatusCode != http.StatusInsufficientStorage || !strings.Contains(string(body), `"quota_exceeded"`) {
		t.Errorf("status = %d, cuerpo = %s; esperado 507 quota_exceeded", over.StatusCode, body)
	}
	if strings.Contains(string(body), "100") || strings.Contains(string(body), "60") {
		t.Errorf("un anónimo no debe averiguar la cuota ni el uso del propietario: %s", body)
	}
}

// Jerarquía de §24 por la API: cuota propia -> grupo -> global.
func TestQuotaFromAGroupAndTheGlobalDefaultThroughTheAPI(t *testing.T) {
	e := newQuotaEnv(t, 1000) // cuota global de 1000 bytes

	if q := e.quota(e.member); q.LimitBytes == nil || *q.LimitBytes != 1000 || q.Source != "global" {
		t.Fatalf("sin nada propio ni de grupo = %+v, esperado la global (1000)", q)
	}

	resp := e.c.do(http.MethodPost, "/api/v1/groups", map[string]any{"name": "Familia", "quota_bytes": 200}, e.admin)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("crear grupo con cuota: status = %d, cuerpo = %s", resp.StatusCode, b)
	}
	group := decodeJSON[groupQuotaDTO](t, resp)
	if group.QuotaBytes == nil || *group.QuotaBytes != 200 {
		t.Errorf("quota_bytes del grupo = %v, esperado 200", group.QuotaBytes)
	}
	resp = e.c.do(http.MethodPost, "/api/v1/groups/"+group.ID+"/members", map[string]string{"user_id": e.memberID}, e.admin)
	resp.Body.Close()

	if q := e.quota(e.member); q.LimitBytes == nil || *q.LimitBytes != 200 || q.Source != "group" || q.GroupName != "Familia" {
		t.Errorf("con grupo = %+v, esperado 200 del grupo Familia", q)
	}

	patchGroup := func(quota any) *http.Response {
		return e.c.do(http.MethodPatch, "/api/v1/groups/"+group.ID, map[string]any{"quota_bytes": quota}, e.admin)
	}
	resp = patchGroup(0)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH grupo 0: status = %d", resp.StatusCode)
	}
	if q := e.quota(e.member); q.LimitBytes != nil || q.Source != "group" {
		t.Errorf("grupo ilimitado = %+v, esperado sin límite con origen group", q)
	}

	resp = patchGroup(nil)
	resp.Body.Close()
	if q := e.quota(e.member); q.LimitBytes == nil || *q.LimitBytes != 1000 || q.Source != "global" {
		t.Errorf("tras quitar la cuota del grupo = %+v, esperado volver a la global", q)
	}

	e.mustSetUserQuota(e.memberID, 50)
	if q := e.quota(e.member); q.LimitBytes == nil || *q.LimitBytes != 50 || q.Source != "user" {
		t.Errorf("con cuota propia = %+v, esperado 50 del usuario", q)
	}

	for name, body := range map[string]map[string]any{
		"negativa":      {"quota_bytes": -1},
		"sin el campo":  {},
		"campo extraño": {"quota_bytes": 5, "nombre": "x"},
		"texto":         {"quota_bytes": "5"},
	} {
		resp := e.c.do(http.MethodPatch, "/api/v1/groups/"+group.ID, body, e.admin)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PATCH grupo con %s: status = %d, esperado 400", name, resp.StatusCode)
		}
	}
	resp = e.c.do(http.MethodPatch, "/api/v1/groups/no-existe", map[string]any{"quota_bytes": 5}, e.admin)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("PATCH de un grupo inexistente: status = %d, esperado 404", resp.StatusCode)
	}
	resp = e.c.do(http.MethodPatch, "/api/v1/groups/"+group.ID, map[string]any{"quota_bytes": 5}, e.member)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("un usuario normal cambiando la cuota de un grupo: status = %d, esperado 403", resp.StatusCode)
	}
}

func TestQuotaChangesAreAudited(t *testing.T) {
	e := newQuotaEnv(t, 0)
	e.mustSetUserQuota(e.memberID, 100)
	e.mustSetUserQuota(e.memberID, nil)

	events, err := audit.NewSQLRepository(db.Wrap("sqlite", e.srv.DB)).ListEvents(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("ListEvents falló: %v", err)
	}
	var changes []*audit.Event
	for _, ev := range events {
		if ev.EventType == audit.EventQuotaChanged {
			changes = append(changes, ev)
		}
	}
	if len(changes) != 2 {
		t.Fatalf("eventos quota_changed = %d, esperado 2", len(changes))
	}
	// ListEvents devuelve el más reciente primero.
	first, second := changes[1], changes[0]
	for _, ev := range changes {
		if ev.ActorUserID != e.adminID || ev.TargetType != "user" || ev.TargetID != e.memberID {
			t.Errorf("evento = actor %q, objetivo %s/%s; esperado la administradora sobre el usuario %s", ev.ActorUserID, ev.TargetType, ev.TargetID, e.memberID)
		}
	}
	if first.Metadata["before"] != nil || first.Metadata["after"] != float64(100) {
		t.Errorf("primer cambio = %v, esperado de sin cuota a 100", first.Metadata)
	}
	if second.Metadata["before"] != float64(100) || second.Metadata["after"] != nil {
		t.Errorf("segundo cambio = %v, esperado de 100 a sin cuota", second.Metadata)
	}
}
