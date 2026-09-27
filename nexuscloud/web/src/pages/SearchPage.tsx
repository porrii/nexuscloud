import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, ApiClientError, type ListResult, type SearchFilters } from '../api/client'
import { useAuth } from '../auth/AuthContext'

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
}

// fullPath junta parent_path+name en una ruta lógica legible -- a
// diferencia de FilesPage/FavoritesPage (una sola carpeta), la búsqueda
// cruza todo el árbol, así que cada resultado necesita mostrar DÓNDE está.
function fullPath(parentPath: string, name: string): string {
  return parentPath === '/' ? `/${name}` : `${parentPath}/${name}`
}

/**
 * Búsqueda de metadatos (§33): recursiva sobre todo el árbol propio, con
 * filtros de tipo/extensión/fecha/tamaño. Si el usuario es admin, un
 * interruptor extra cruza todos los usuarios (o uno concreto por username).
 */
export default function SearchPage() {
  const { user } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()

  const [query, setQuery] = useState(searchParams.get('q') ?? '')
  const [type, setType] = useState('')
  const [ext, setExt] = useState('')
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [sizeMinMB, setSizeMinMB] = useState('')
  const [sizeMaxMB, setSizeMaxMB] = useState('')
  const [adminMode, setAdminMode] = useState(false)
  const [owner, setOwner] = useState('')

  const [result, setResult] = useState<ListResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const runSearch = useCallback(
    async (q: string) => {
      setLoading(true)
      setError(null)
      const filters: SearchFilters = {
        q: q || undefined,
        type: type || undefined,
        ext: ext || undefined,
        date_from: dateFrom ? new Date(dateFrom).toISOString() : undefined,
        date_to: dateTo ? new Date(dateTo).toISOString() : undefined,
        size_min: sizeMinMB ? Math.round(Number(sizeMinMB) * 1024 * 1024) : undefined,
        size_max: sizeMaxMB ? Math.round(Number(sizeMaxMB) * 1024 * 1024) : undefined,
      }
      try {
        setResult(adminMode ? await api.searchAsAdmin(filters, owner || undefined) : await api.search(filters))
      } catch (err) {
        setError(err instanceof ApiClientError ? err.message : 'No se pudo completar la búsqueda.')
      } finally {
        setLoading(false)
      }
    },
    [type, ext, dateFrom, dateTo, sizeMinMB, sizeMaxMB, adminMode, owner],
  )

  // Solo se dispara al llegar desde la barra de búsqueda de AppShell (?q=) o
  // al cambiar de esa query en la URL -- ajustar un filtro y pulsar Buscar
  // no toca la URL, evita una entrada de historial por cada tecleo.
  useEffect(() => {
    const q = searchParams.get('q') ?? ''
    setQuery(q)
    if (q) void runSearch(q)
    // runSearch depende de los filtros a propósito -- fuera de esta lista:
    // cambiarlos no debe disparar una búsqueda hasta pulsar "Buscar", solo
    // llegar con un ?q= nuevo desde la barra de AppShell.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  function handleSearchClick() {
    setSearchParams(query ? { q: query } : {})
    void runSearch(query)
  }

  return (
    <div className="flex h-full flex-col p-6">
      <h1 className="mb-4 text-lg font-semibold text-slate-900 dark:text-slate-50">Búsqueda</h1>

      <div className="mb-4 space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="flex gap-2">
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearchClick()}
            placeholder="Nombre o ruta…"
            className="flex-1 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <button
            onClick={handleSearchClick}
            disabled={loading}
            className="rounded-md bg-blue-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {loading ? 'Buscando…' : 'Buscar'}
          </button>
        </div>

        <div className="flex flex-wrap gap-2">
          <select
            value={type}
            onChange={(e) => setType(e.target.value)}
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          >
            <option value="">Cualquier tipo</option>
            <option value="image/">Imágenes</option>
            <option value="video/">Vídeos</option>
            <option value="audio/">Audio</option>
            <option value="application/pdf">PDF</option>
            <option value="text/">Texto</option>
          </select>
          <input
            value={ext}
            onChange={(e) => setExt(e.target.value)}
            placeholder="Extensión (docx, zip…)"
            className="w-40 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <input
            type="date"
            value={dateFrom}
            onChange={(e) => setDateFrom(e.target.value)}
            title="Desde"
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <input
            type="date"
            value={dateTo}
            onChange={(e) => setDateTo(e.target.value)}
            title="Hasta"
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <input
            type="number"
            min={0}
            value={sizeMinMB}
            onChange={(e) => setSizeMinMB(e.target.value)}
            placeholder="MB mín."
            className="w-24 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <input
            type="number"
            min={0}
            value={sizeMaxMB}
            onChange={(e) => setSizeMaxMB(e.target.value)}
            placeholder="MB máx."
            className="w-24 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
        </div>

        {user?.is_admin && (
          <div className="flex items-center gap-2 border-t border-slate-100 pt-3 dark:border-slate-800">
            <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
              <input type="checkbox" checked={adminMode} onChange={(e) => setAdminMode(e.target.checked)} />
              Buscar en toda la instancia
            </label>
            {adminMode && (
              <input
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
                placeholder="Usuario (opcional, todos si se deja vacío)"
                className="flex-1 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
              />
            )}
          </div>
        )}
      </div>

      <div className="flex-1 overflow-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        {error && (
          <div className="m-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
            {error}
          </div>
        )}
        {!result ? (
          <p className="p-12 text-center text-sm text-slate-500 dark:text-slate-400">Escribe algo y pulsa Buscar.</p>
        ) : result.directories.length === 0 && result.files.length === 0 ? (
          <p className="p-12 text-center text-sm text-slate-500 dark:text-slate-400">Sin resultados.</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {result.directories.map((d) => (
              <li key={d.id} className="flex items-center justify-between px-4 py-3 text-sm">
                <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                  <span aria-hidden>📁</span>
                  {d.name}
                </span>
                <span className="truncate pl-4 text-xs text-slate-400">{fullPath(d.parent_path, d.name)}</span>
              </li>
            ))}
            {result.files.map((f) => (
              <li key={f.id} className="flex items-center justify-between px-4 py-3 text-sm">
                <span className="flex min-w-0 items-center gap-2 text-slate-800 dark:text-slate-200">
                  <span aria-hidden>📄</span>
                  <span className="truncate">{f.name}</span>
                  <span className="shrink-0 text-xs text-slate-400">{formatBytes(f.size_bytes)}</span>
                  <span className="shrink-0 text-xs text-slate-400">{formatDate(f.created_at)}</span>
                </span>
                <div className="flex shrink-0 items-center gap-3 pl-4">
                  <span className="truncate text-xs text-slate-400">{fullPath(f.parent_path, f.name)}</span>
                  <a
                    href={api.downloadUrl(f.id)}
                    download={f.name}
                    className="rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                  >
                    Descargar
                  </a>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
