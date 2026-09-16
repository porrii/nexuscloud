// Package clientupdates reenvía el feed y los paquetes de actualización del
// cliente de escritorio (Velopack) desde las Releases de GitHub del propio
// repositorio del proyecto, para que el cliente instalado nunca necesite un
// token de GitHub propio -- mismo motivo de fondo que
// backup.RemoteDestination/RemoteToken (ADR-029): el secreto vive solo en el
// servidor. Ver ADR-032.
package clientupdates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ErrAssetNotFound se devuelve cuando el asset pedido no está en la última
// release del canal configurado -- el llamador HTTP lo traduce a 404.
var ErrAssetNotFound = errors.New("clientupdates: asset no encontrado")

// feedCacheTTL evita gastar el límite de peticiones de la API de GitHub en
// cada arranque de cada cliente: la lista de releases de este proyecto no
// cambia más que unas pocas veces al mes.
const feedCacheTTL = 5 * time.Minute

// releasesPerPage acota cuántas releases recientes se examinan buscando la
// última que contenga un feed de Velopack -- de sobra si este repositorio
// alguna vez publica también otro tipo de release (p.ej. binarios del
// servidor) entre dos releases del cliente.
const releasesPerPage = 10

// Proxy reenvía el feed de Velopack (releases.<channel>.json) y sus assets
// desde las Releases de un único repositorio de GitHub.
type Proxy struct {
	repo    string // "propietario/repositorio"
	channel string
	token   string
	client  *http.Client
	apiBase string // sobreescribible en tests, por defecto githubAPIBase

	mu       sync.Mutex
	cached   *githubRelease
	cachedAt time.Time
}

const githubAPIBase = "https://api.github.com"

func NewProxy(repo, channel, token string) *Proxy {
	return &Proxy{
		repo:    repo,
		channel: channel,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
		apiBase: githubAPIBase,
	}
}

type githubRelease struct {
	Assets []githubAsset `json:"assets"`
}

type githubAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// FeedJSON devuelve el contenido crudo de releases.<channel>.json de la
// última release del repositorio configurado que tenga ese fichero.
func (p *Proxy) FeedJSON(ctx context.Context) ([]byte, error) {
	rel, err := p.latestRelease(ctx)
	if err != nil {
		return nil, err
	}
	asset := findAsset(rel, p.feedAssetName())
	if asset == nil {
		return nil, fmt.Errorf("%w: %s", ErrAssetNotFound, p.feedAssetName())
	}
	body, _, err := p.downloadAsset(ctx, asset)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

// Asset descarga un asset por su nombre EXACTO dentro de la misma última
// release ya resuelta -- nunca acepta una URL o ruta arbitraria del
// llamador, para que el proxy no se pueda usar para pedirle a GitHub
// cualquier otro fichero del repositorio.
func (p *Proxy) Asset(ctx context.Context, name string) (io.ReadCloser, int64, error) {
	rel, err := p.latestRelease(ctx)
	if err != nil {
		return nil, 0, err
	}
	asset := findAsset(rel, name)
	if asset == nil {
		return nil, 0, ErrAssetNotFound
	}
	return p.downloadAsset(ctx, asset)
}

func (p *Proxy) feedAssetName() string {
	return fmt.Sprintf("releases.%s.json", p.channel)
}

func findAsset(rel *githubRelease, name string) *githubAsset {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}
	return nil
}

// latestRelease resuelve (con caché de feedCacheTTL) la release más
// reciente del repositorio que contenga un feed de Velopack para el canal
// configurado, examinando como mucho las últimas releasesPerPage.
func (p *Proxy) latestRelease(ctx context.Context) (*githubRelease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != nil && time.Since(p.cachedAt) < feedCacheTTL {
		return p.cached, nil
	}

	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", p.apiBase, p.repo, releasesPerPage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	p.setAuthHeaders(req, "application/vnd.github+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clientupdates: consultando releases de %s: %w", p.repo, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clientupdates: GitHub devolvió %d listando releases de %s", resp.StatusCode, p.repo)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("clientupdates: decodificando releases de %s: %w", p.repo, err)
	}

	feedName := p.feedAssetName()
	for i := range releases {
		if findAsset(&releases[i], feedName) != nil {
			p.cached = &releases[i]
			p.cachedAt = time.Now()
			return p.cached, nil
		}
	}
	return nil, fmt.Errorf("clientupdates: ninguna de las últimas %d releases de %s tiene %s", releasesPerPage, p.repo, feedName)
}

// downloadAsset pide el contenido binario de un asset ya localizado. Se usa
// el endpoint /releases/assets/{id} (no browser_download_url) porque es el
// único que funciona autenticado contra un repositorio privado.
//
// El cliente HTTP por defecto de Go NO reenvía la cabecera Authorization al
// seguir una redirección hacia otro host -- y GitHub responde aquí con una
// redirección 302 hacia una URL firmada de almacenamiento de blobs, un host
// distinto. Es justo el comportamiento que se necesita (nunca hay que
// mandarle nuestro token de GitHub a ese otro host), así que no hace falta
// ningún CheckRedirect a medida.
func (p *Proxy) downloadAsset(ctx context.Context, asset *githubAsset) (io.ReadCloser, int64, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/assets/%d", p.apiBase, p.repo, asset.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	p.setAuthHeaders(req, "application/octet-stream")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("clientupdates: descargando asset %q: %w", asset.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("clientupdates: GitHub devolvió %d descargando el asset %q", resp.StatusCode, asset.Name)
	}
	return resp.Body, resp.ContentLength, nil
}

func (p *Proxy) setAuthHeaders(req *http.Request, accept string) {
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
}
