package integration

import (
	"net/http"
	"testing"
)

// Favoritos (§87) y actividad reciente (§88), ADR-038: mismo servidor real
// que el resto de este paquete.

func TestFavoriteCreateListDeleteAndAnnotatesFileListing(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "ana", "contraseña-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, c, "ana", "contraseña-larga-123")

	file := uploadTestFile(t, c, token, "/", "informe.pdf", "contenido")
	dirResp := c.do(http.MethodPost, "/api/v1/directories", map[string]string{"parent_path": "/", "name": "Fotos"}, token)
	dir := decodeJSON[directoryDTO](t, dirResp)

	resp := c.do(http.MethodPost, "/api/v1/favorites", map[string]string{"resource_type": "file", "resource_id": file.ID}, token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("crear favorito: status = %d", resp.StatusCode)
	}
	created := decodeJSON[struct {
		ID           string `json:"id"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
	}](t, resp)
	if created.ResourceType != "file" || created.ResourceID != file.ID {
		t.Errorf("favorito creado = %+v", created)
	}

	// GET /files anota favorite_id en la fila favorita y lo deja ausente en
	// la que no lo es.
	resp = c.do(http.MethodGet, "/api/v1/files?path=/", nil, token)
	listing := decodeJSON[struct {
		Directories []struct {
			ID         string  `json:"id"`
			FavoriteID *string `json:"favorite_id"`
		} `json:"directories"`
		Files []struct {
			ID         string  `json:"id"`
			FavoriteID *string `json:"favorite_id"`
		} `json:"files"`
	}](t, resp)
	if len(listing.Files) != 1 || listing.Files[0].FavoriteID == nil || *listing.Files[0].FavoriteID != created.ID {
		t.Errorf("GET /files no anotó favorite_id en el archivo: %+v", listing.Files)
	}
	if len(listing.Directories) != 1 || listing.Directories[0].ID != dir.ID || listing.Directories[0].FavoriteID != nil {
		t.Errorf("la carpeta no favorita no debería llevar favorite_id: %+v", listing.Directories)
	}

	// GET /favorites la lista en su propia página.
	resp = c.do(http.MethodGet, "/api/v1/favorites", nil, token)
	favList := decodeJSON[struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}](t, resp)
	if len(favList.Files) != 1 || favList.Files[0].ID != file.ID {
		t.Errorf("GET /favorites = %+v", favList)
	}

	resp = c.do(http.MethodDelete, "/api/v1/favorites/"+created.ID, nil, token)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("quitar favorito: status = %d", resp.StatusCode)
	}
	resp = c.do(http.MethodGet, "/api/v1/files?path=/", nil, token)
	listing = decodeJSON[struct {
		Directories []struct {
			ID         string  `json:"id"`
			FavoriteID *string `json:"favorite_id"`
		} `json:"directories"`
		Files []struct {
			ID         string  `json:"id"`
			FavoriteID *string `json:"favorite_id"`
		} `json:"files"`
	}](t, resp)
	if listing.Files[0].FavoriteID != nil {
		t.Errorf("tras quitar el favorito, favorite_id debería quedar ausente: %+v", listing.Files)
	}
}

func TestFavoriteCreateIsIdempotentOverHTTP(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "ana", "contraseña-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, c, "ana", "contraseña-larga-123")
	file := uploadTestFile(t, c, token, "/", "informe.pdf", "contenido")

	body := map[string]string{"resource_type": "file", "resource_id": file.ID}
	first := decodeJSON[struct {
		ID string `json:"id"`
	}](t, c.do(http.MethodPost, "/api/v1/favorites", body, token))
	second := decodeJSON[struct {
		ID string `json:"id"`
	}](t, c.do(http.MethodPost, "/api/v1/favorites", body, token))
	if first.ID != second.ID {
		t.Errorf("favoritar dos veces devolvió IDs distintos: %q vs %q", first.ID, second.ID)
	}
}

func TestFavoriteCreateRejectsResourceOwnedByAnotherUser(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "ana", "contraseña-larga-123")
	createAdmin(t, srv, "bea", "contraseña-larga-456")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	anaToken := loginAndGetToken(t, c, "ana", "contraseña-larga-123")
	beaToken := loginAndGetToken(t, c, "bea", "contraseña-larga-456")
	file := uploadTestFile(t, c, anaToken, "/", "informe.pdf", "contenido")

	resp := c.do(http.MethodPost, "/api/v1/favorites", map[string]string{"resource_type": "file", "resource_id": file.ID}, beaToken)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("favoritar el archivo de otro: status = %d, esperado 403", resp.StatusCode)
	}
}

func TestFavoriteDeleteRequiresOwnerIDOR(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "ana", "contraseña-larga-123")
	createAdmin(t, srv, "bea", "contraseña-larga-456")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	anaToken := loginAndGetToken(t, c, "ana", "contraseña-larga-123")
	beaToken := loginAndGetToken(t, c, "bea", "contraseña-larga-456")
	file := uploadTestFile(t, c, anaToken, "/", "informe.pdf", "contenido")
	created := decodeJSON[struct {
		ID string `json:"id"`
	}](t, c.do(http.MethodPost, "/api/v1/favorites", map[string]string{"resource_type": "file", "resource_id": file.ID}, anaToken))

	resp := c.do(http.MethodDelete, "/api/v1/favorites/"+created.ID, nil, beaToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("quitar el favorito de otro: status = %d, esperado 404", resp.StatusCode)
	}
}

func TestFavoritesRequireAuth(t *testing.T) {
	ts, _ := newTestServer(t)
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}

	for _, call := range []func() *http.Response{
		func() *http.Response { return c.do(http.MethodGet, "/api/v1/favorites", nil, "") },
		func() *http.Response {
			return c.do(http.MethodPost, "/api/v1/favorites", map[string]string{"resource_type": "file", "resource_id": "x"}, "")
		},
		func() *http.Response { return c.do(http.MethodDelete, "/api/v1/favorites/x", nil, "") },
		func() *http.Response { return c.do(http.MethodGet, "/api/v1/activity", nil, "") },
	} {
		if resp := call(); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("sin sesión: status = %d, esperado 401", resp.StatusCode)
		}
	}
}

func TestActivityListsRecentEventsForCallerOnly(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "ana", "contraseña-larga-123")
	createAdmin(t, srv, "bea", "contraseña-larga-456")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	anaToken := loginAndGetToken(t, c, "ana", "contraseña-larga-123")
	beaToken := loginAndGetToken(t, c, "bea", "contraseña-larga-456")

	uploadTestFile(t, c, anaToken, "/", "informe.pdf", "contenido")

	resp := c.do(http.MethodGet, "/api/v1/activity", nil, anaToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /activity (ana): status = %d", resp.StatusCode)
	}
	anaEvents := decodeJSON[[]struct {
		EventType string         `json:"event_type"`
		Metadata  map[string]any `json:"metadata"`
	}](t, resp)
	if len(anaEvents) != 1 || anaEvents[0].EventType != "upload" {
		t.Fatalf("actividad de ana = %+v, esperado 1 evento 'upload'", anaEvents)
	}
	if anaEvents[0].Metadata["name"] != "informe.pdf" {
		t.Errorf("metadata del evento de subida = %+v, esperaba name=informe.pdf", anaEvents[0].Metadata)
	}
	for _, e := range anaEvents {
		if e.EventType == "login" {
			t.Error("el login no debe aparecer en la actividad reciente (§88 no es eso)")
		}
	}

	resp = c.do(http.MethodGet, "/api/v1/activity", nil, beaToken)
	beaEvents := decodeJSON[[]map[string]any](t, resp)
	if len(beaEvents) != 0 {
		t.Errorf("actividad de bea = %+v, esperada vacía (no debe ver la subida de ana)", beaEvents)
	}
}

func TestActivityRespectsLimit(t *testing.T) {
	ts, srv := newTestServer(t)
	createAdmin(t, srv, "ana", "contraseña-larga-123")
	c := &apiClient{t: t, base: ts.URL, client: ts.Client()}
	token := loginAndGetToken(t, c, "ana", "contraseña-larga-123")

	uploadTestFile(t, c, token, "/", "uno.txt", "1")
	uploadTestFile(t, c, token, "/", "dos.txt", "2")
	uploadTestFile(t, c, token, "/", "tres.txt", "3")

	resp := c.do(http.MethodGet, "/api/v1/activity?limit=1", nil, token)
	events := decodeJSON[[]map[string]any](t, resp)
	if len(events) != 1 {
		t.Errorf("limit=1 devolvió %d eventos, esperado 1", len(events))
	}
}
