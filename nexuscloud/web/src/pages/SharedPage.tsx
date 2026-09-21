import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiClientError, type Share, type SharedDirectoryListing } from '../api/client'

interface UploadProgress {
  key: string
  name: string
  percent: number
  error?: string
}

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

function tooLargeMessage(maxBytes?: number): string {
  return maxBytes !== undefined ? `Supera el límite de ${formatBytes(maxBytes)} por archivo.` : 'Supera el límite de tamaño de esta carpeta.'
}

// Mensajes de la subida a una carpeta compartida (§37, ADR-035): el servidor
// no sobrescribe un archivo existente, así que un nombre ya usado es un error esperable.
function sharedUploadErrorMessage(err: unknown, maxBytes?: number): string {
  if (!(err instanceof ApiClientError)) return 'Error al subir el archivo.'
  switch (err.code) {
    case 'destination_occupied':
      return 'Ya hay un archivo con ese nombre en esta carpeta; no se sobrescribe.'
    case 'upload_too_large':
      return tooLargeMessage(maxBytes)
    case 'upload_not_allowed':
    case 'forbidden':
      return 'Ya no tienes permiso para subir a esta carpeta.'
    default:
      return err.message
  }
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
  const [browseResult, setBrowseResult] = useState<SharedDirectoryListing | null>(null)
  const [browseLoading, setBrowseLoading] = useState(false)
  const [uploads, setUploads] = useState<UploadProgress[]>([])
  const fileInputRef = useRef<HTMLInputElement>(null)
  // Carpeta que se está viendo ahora mismo: una subida que termina cuando ya se
  // ha navegado a otra no debe pisar el listado con el de la carpeta anterior.
  const currentDirId = useRef<string | undefined>(undefined)

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

  useEffect(() => {
    currentDirId.current = browseStack[browseStack.length - 1]?.id
  }, [browseStack])

  // Los errores de subida son de la carpeta anterior: al navegar se descartan
  // (las subidas en curso siguen).
  function dropFailedUploads() {
    setUploads((prev) => prev.filter((u) => !u.error))
  }

  async function openDirectory(id: string, name: string) {
    setBrowseLoading(true)
    setError(null)
    dropFailedUploads()
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
    dropFailedUploads()
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

  // Sube uno a uno (un XHR con progreso por archivo) a la carpeta que se está
  // viendo, y al terminar refresca el listado -- también tras un error, por si
  // el permiso cambió mientras tanto (el botón desaparecería).
  async function handleUploadFiles(files: FileList | File[]) {
    const dirId = browseStack[browseStack.length - 1]?.id
    if (!dirId) return
    const maxBytes = browseResult?.max_upload_size_bytes
    for (const file of Array.from(files)) {
      const key = `${file.name}-${Date.now()}`
      if (maxBytes !== undefined && file.size > maxBytes) {
        // No se manda un archivo que el servidor va a rechazar a medio subir.
        setUploads((prev) => [...prev, { key, name: file.name, percent: 0, error: tooLargeMessage(maxBytes) }])
        continue
      }
      setUploads((prev) => [...prev, { key, name: file.name, percent: 0 }])
      try {
        await api.uploadToSharedDirectory(dirId, file.name, file, (percent) => {
          setUploads((prev) => prev.map((u) => (u.key === key ? { ...u, percent } : u)))
        })
        setUploads((prev) => prev.filter((u) => u.key !== key))
      } catch (err) {
        const message = sharedUploadErrorMessage(err, maxBytes)
        setUploads((prev) => prev.map((u) => (u.key === key ? { ...u, error: message } : u)))
      }
    }
    try {
      const fresh = await api.listSharedDirectory(dirId)
      if (currentDirId.current === dirId) setBrowseResult(fresh)
    } catch {
      // El listado se refrescará en la próxima navegación; el resultado de cada subida ya se ve arriba.
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
      const withUpload = s.can_upload ? ' · con subida' : ''
      if (s.share_type === 'user') return `Con ${s.target_username ?? 'un usuario'}${withUpload}`
      if (s.share_type === 'group') return `Con el grupo ${s.target_group_name ?? ''}${withUpload}`
      return (s.label ? `Enlace: ${s.label}` : 'Enlace público') + withUpload
    }
    if (s.resource_type === 'directory') return s.can_upload ? 'Carpeta compartida · puedes subir' : 'Carpeta compartida'
    return 'Archivo compartido'
  }

  const isBrowsing = browseStack.length > 0
  const canUploadHere = isBrowsing && browseResult?.can_upload === true
  const uploading = uploads.some((u) => !u.error)

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
        <div className="mb-3 flex items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-1 text-sm text-slate-500 dark:text-slate-400">
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
          {canUploadHere && (
            <>
              <input
                ref={fileInputRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  if (e.target.files?.length) void handleUploadFiles(e.target.files)
                  e.target.value = ''
                }}
              />
              <button
                disabled={uploading}
                onClick={() => fileInputRef.current?.click()}
                className="shrink-0 rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              >
                {uploading ? 'Subiendo…' : 'Subir archivo'}
              </button>
            </>
          )}
        </div>
      )}

      {canUploadHere && (
        <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">
          Puedes subir archivos a esta carpeta; los que ya existen no se sobrescriben.
          {browseResult?.max_upload_size_bytes !== undefined && ` Máximo ${formatBytes(browseResult.max_upload_size_bytes)} por archivo.`}
        </p>
      )}

      {uploads.length > 0 && (
        <div className="mb-4 space-y-1.5">
          {uploads.map((u) => (
            <div key={u.key} className="rounded-md border border-slate-200 bg-white px-3 py-2 text-sm dark:border-slate-800 dark:bg-slate-900">
              <div className="flex justify-between gap-3">
                <span className="truncate text-slate-700 dark:text-slate-300">{u.name}</span>
                <span className={u.error ? 'text-red-600 dark:text-red-400' : 'text-slate-500'}>{u.error ?? `${u.percent}%`}</span>
              </div>
              {!u.error && (
                <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
                  <div className="h-full bg-blue-600 transition-all" style={{ width: `${u.percent}%` }} />
                </div>
              )}
            </div>
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
