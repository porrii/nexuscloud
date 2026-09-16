package clientupdates

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// newTestServer simula la API de GitHub para /releases y
// /releases/assets/{id}, y cuenta cuántas veces se llamó a /releases (para
// verificar la caché). requireToken, si no está vacío, exige que llegue
// exactamente esa cabecera Authorization -- si no, responde 401.
func newTestServer(t *testing.T, releasesJSON string, assetBodies map[int64]string, requireToken string) (*httptest.Server, *int32) {
	t.Helper()
	var releasesCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&releasesCalls, 1)
		if requireToken != "" && r.Header.Get("Authorization") != "Bearer "+requireToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept inesperado en /releases: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(releasesJSON))
	})
	mux.HandleFunc("/repos/owner/repo/releases/assets/", func(w http.ResponseWriter, r *http.Request) {
		if requireToken != "" && r.Header.Get("Authorization") != "Bearer "+requireToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Errorf("Accept inesperado en /releases/assets: %q", got)
		}
		idStr := r.URL.Path[len("/repos/owner/repo/releases/assets/"):]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, ok := assetBodies[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &releasesCalls
}

func newProxyForTest(srv *httptest.Server, token string) *Proxy {
	p := NewProxy("owner/repo", "win", token)
	p.apiBase = srv.URL
	return p
}

const oneReleaseWithFeed = `[{"assets":[
  {"id":1,"name":"releases.win.json"},
  {"id":2,"name":"NexusCloud-1.0.0-full.nupkg"}
]}]`

func TestFeedJSONReturnsFeedAssetContent(t *testing.T) {
	srv, _ := newTestServer(t, oneReleaseWithFeed, map[int64]string{1: `{"Assets":[]}`}, "")
	p := newProxyForTest(srv, "")

	got, err := p.FeedJSON(context.Background())
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	if string(got) != `{"Assets":[]}` {
		t.Errorf("contenido del feed = %q", got)
	}
}

func TestAssetReturnsExactNamedAssetContent(t *testing.T) {
	srv, _ := newTestServer(t, oneReleaseWithFeed, map[int64]string{2: "contenido-del-paquete"}, "")
	p := newProxyForTest(srv, "")

	body, size, err := p.Asset(context.Background(), "NexusCloud-1.0.0-full.nupkg")
	if err != nil {
		t.Fatalf("Asset: %v", err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != "contenido-del-paquete" {
		t.Errorf("contenido = %q", got)
	}
	if size != int64(len("contenido-del-paquete")) {
		t.Errorf("size = %d", size)
	}
}

func TestAssetUnknownNameReturnsErrAssetNotFound(t *testing.T) {
	srv, _ := newTestServer(t, oneReleaseWithFeed, map[int64]string{}, "")
	p := newProxyForTest(srv, "")

	_, _, err := p.Asset(context.Background(), "no-existe.nupkg")
	if !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("err = %v, esperaba ErrAssetNotFound", err)
	}
}

func TestFeedJSONSendsBearerToken(t *testing.T) {
	srv, _ := newTestServer(t, oneReleaseWithFeed, map[int64]string{1: `{}`}, "s3cr3t")
	p := newProxyForTest(srv, "s3cr3t")

	if _, err := p.FeedJSON(context.Background()); err != nil {
		t.Fatalf("FeedJSON con token correcto: %v", err)
	}

	pSinToken := newProxyForTest(srv, "")
	pSinToken.cached = nil
	if _, err := pSinToken.FeedJSON(context.Background()); err == nil {
		t.Errorf("esperaba error sin token contra un repo que lo exige")
	}
}

func TestLatestReleaseSkipsReleasesWithoutFeedAsset(t *testing.T) {
	// La release más reciente (primera del array, igual que la API real de
	// GitHub) no tiene releases.win.json -- p.ej. una release de otra cosa
	// intercalada. El proxy debe seguir buscando hacia atrás, nunca
	// confiar a ciegas en "la más reciente".
	const releases = `[
	  {"assets":[{"id":9,"name":"algo-no-relacionado.txt"}]},
	  {"assets":[{"id":1,"name":"releases.win.json"}]}
	]`
	srv, _ := newTestServer(t, releases, map[int64]string{1: `{"Assets":[]}`}, "")
	p := newProxyForTest(srv, "")

	got, err := p.FeedJSON(context.Background())
	if err != nil {
		t.Fatalf("FeedJSON: %v", err)
	}
	if string(got) != `{"Assets":[]}` {
		t.Errorf("debía encontrar el feed en la segunda release, got %q", got)
	}
}

func TestLatestReleaseIsCachedWithinTTL(t *testing.T) {
	srv, calls := newTestServer(t, oneReleaseWithFeed, map[int64]string{1: `{}`, 2: `{}`}, "")
	p := newProxyForTest(srv, "")

	if _, err := p.FeedJSON(context.Background()); err != nil {
		t.Fatalf("primera llamada: %v", err)
	}
	if _, _, err := p.Asset(context.Background(), "NexusCloud-1.0.0-full.nupkg"); err != nil {
		t.Fatalf("segunda llamada: %v", err)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("llamadas reales a /releases = %d, esperaba 1 (debía servirse de caché)", got)
	}
}

func TestNoReleaseWithFeedAssetReturnsClearError(t *testing.T) {
	srv, _ := newTestServer(t, `[{"assets":[{"id":9,"name":"otra-cosa.txt"}]}]`, nil, "")
	p := newProxyForTest(srv, "")

	if _, err := p.FeedJSON(context.Background()); err == nil {
		t.Errorf("esperaba error cuando ninguna release tiene el feed")
	}
}
