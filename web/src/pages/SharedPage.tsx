import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type DirectoryEntry, type FileEntry, type Share } from '../api/client'

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

type Direction = 'with-me' | 'by-me'
interface BrowseEntry {
  id: string
  name: string
}

/**
 * "Compartido conmigo" / "Compartido por mí" (§143). Navegar dentro de una
 * carpeta compartida reutiliza api.listSharedDirectory con el ID real de
 * cada subcarpeta (nunca una sub-ruta que el cliente pudiera manipular) --
 * ver storage.FileService.ListSharedDirectory.
 */
export default function SharedPage() {
  const [direction, setDirection] = useState<Direction>('with-me')
  const [shares, setShares] = useState<Share[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [browseStack, setBrowseStack] = useState<BrowseEntry[]>([])
  const [browseResult, setBrowseResult] = useState<{ directories: DirectoryEntry[]; files: FileEntry[] } | null>(null)
  const [browseLoading, setBrowseLoading] = useState(false)

  const loadShares = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setShares(await api.listShares(direction))
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar las comparticiones.')
    } finally {
      setLoading(false)
    }
  }, [direction])

  useEffect(() => {
    setBrowseStack([])
    setBrowseResult(null)
    void loadShares()
  }, [loadShares])

  async function openDirectory(id: string, name: string) {
    setBrowseLoading(true)
    setError(null)
    try {
      const result = await api.listSharedDirectory(id)
      setBrowseResult(result)
      setBrowseStack((prev) => [...prev, { id, name }])
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo abrir la carpeta compartida.')
    } finally {
      setBrowseLoading(false)
    }
  }

  async function navigateToBrowseIndex(index: number) {
    if (index < 0) {
      setBrowseStack([])
      setBrowseResult(null)
      return
    }
    setBrowseLoading(true)
    setError(null)
    try {
      const target = browseStack[index]
      const result = await api.listSharedDirectory(target.id)
      setBrowseResult(result)
      setBrowseStack((prev) => prev.slice(0, index + 1))
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo abrir la carpeta compartida.')
    } finally {
      setBrowseLoading(false)
    }
  }

  async function handleRevoke(id: string) {
    setError(null)
    try {
      await api.revokeShare(id)
      await loadShares()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo revocar la compartición.')
    }
  }

  function shareOriginLabel(s: Share): string {
    if (direction === 'by-me') {
      if (s.share_type === 'user') return `Con ${s.target_username ?? 'un usuario'}`
      if (s.share_type === 'group') return `Con el grupo ${s.target_group_name ?? ''}`
      return s.label ? `Enlace: ${s.label}` : 'Enlace público'
    }
    return s.resource_type === 'directory' ? 'Carpeta compartida' : 'Archivo compartido'
  }

  const isBrowsing = browseStack.length > 0

  return (
    <div className="flex h-full flex-col p-6">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-50">Compartido</h1>
        <div className="flex gap-1 rounded-md bg-slate-100 p-1 dark:bg-slate-800">
          <button
            onClick={() => setDirection('with-me')}
            className={`rounded px-3 py-1 text-sm font-medium ${
              direction === 'with-me' ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-500 dark:text-slate-400'
            }`}
          >
            Compartido conmigo
          </button>
          <button
            onClick={() => setDirection('by-me')}
            className={`rounded px-3 py-1 text-sm font-medium ${
              direction === 'by-me' ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-500 dark:text-slate-400'
            }`}
          >
            Compartido por mí
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {error}
        </div>
      )}

      {isBrowsing && (
        <div className="mb-3 flex flex-wrap items-center gap-1 text-sm text-slate-500 dark:text-slate-400">
          <button onClick={() => void navigateToBrowseIndex(-1)} className="hover:text-blue-700 dark:hover:text-blue-400">
            Compartido conmigo
          </button>
          {browseStack.map((entry, i) => (
            <span key={entry.id} className="flex items-center gap-1">
              <span>/</span>
              <button onClick={() => void navigateToBrowseIndex(i)} className="hover:text-blue-700 dark:hover:text-blue-400">
                {entry.name}
              </button>
            </span>
          ))}
        </div>
      )}

      <div className="flex-1 overflow-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        {loading || browseLoading ? (
          <p className="p-6 text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : isBrowsing && browseResult ? (
          browseResult.directories.length === 0 && browseResult.files.length === 0 ? (
            <p className="p-6 text-sm text-slate-500 dark:text-slate-400">Esta carpeta está vacía.</p>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800 dark:text-slate-400">
                <tr>
                  <th className="px-4 py-2 font-medium">Nombre</th>
                  <th className="px-4 py-2 font-medium">Tamaño</th>
                  <th className="px-4 py-2 font-medium" />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {browseResult.directories.map((d) => (
                  <tr key={d.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <td className="px-4 py-2">
                      <button
                        onClick={() => void openDirectory(d.id, d.name)}
                        className="flex items-center gap-2 font-medium text-slate-800 hover:text-blue-700 dark:text-slate-200 dark:hover:text-blue-400"
                      >
                        <span aria-hidden>📁</span>
                        {d.name}
                      </button>
                    </td>
                    <td className="px-4 py-2 text-slate-400">—</td>
                    <td className="px-4 py-2" />
                  </tr>
                ))}
                {browseResult.files.map((f) => (
                  <tr key={f.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <td className="px-4 py-2">
                      <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                        <span aria-hidden>📄</span>
                        {f.name}
                      </span>
                    </td>
                    <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatBytes(f.size_bytes)}</td>
                    <td className="px-4 py-2 text-right">
                      <a
                        href={api.downloadUrl(f.id)}
                        download={f.name}
                        className="rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                      >
                        Descargar
                      </a>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        ) : shares.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-1 p-12 text-center">
            <p className="text-sm font-medium text-slate-600 dark:text-slate-300">
              {direction === 'with-me' ? 'Nadie ha compartido nada contigo todavía' : 'Todavía no has compartido nada'}
            </p>
          </div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800 dark:text-slate-400">
              <tr>
                <th className="px-4 py-2 font-medium">Nombre</th>
                <th className="px-4 py-2 font-medium">Origen</th>
                <th className="px-4 py-2 font-medium">Creado</th>
                <th className="px-4 py-2 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {shares.map((s) => (
                <tr key={s.id} className="group hover:bg-slate-50 dark:hover:bg-slate-800/50">
                  <td className="px-4 py-2">
                    {direction === 'with-me' && s.resource_type === 'directory' ? (
                      <button
                        onClick={() => void openDirectory(s.resource_id, s.resource_name ?? '')}
                        className="flex items-center gap-2 font-medium text-slate-800 hover:text-blue-700 dark:text-slate-200 dark:hover:text-blue-400"
                      >
                        <span aria-hidden>📁</span>
                        {s.resource_name ?? s.resource_id}
                      </button>
                    ) : (
                      <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                        <span aria-hidden>{s.resource_type === 'directory' ? '📁' : '📄'}</span>
                        {s.resource_name ?? s.resource_id}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{shareOriginLabel(s)}</td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatDate(s.created_at)}</td>
                  <td className="px-4 py-2 text-right">
                    {direction === 'with-me' && s.resource_type === 'file' && (
                      <a
                        href={api.downloadUrl(s.resource_id)}
                        download={s.resource_name}
                        className="mr-3 rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                      >
                        Descargar
                      </a>
                    )}
                    {direction === 'by-me' && (
                      <button
                        onClick={() => void handleRevoke(s.id)}
                        className="invisible rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 group-hover:visible dark:text-red-400 dark:hover:bg-red-950"
                      >
                        Revocar
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
