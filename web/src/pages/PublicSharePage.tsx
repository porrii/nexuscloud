import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiClientError, type DirectoryEntry, type FileEntry, type PublicShareInfo } from '../api/client'

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

/**
 * Landing de un enlace público (§37) -- fuera de RequireAuth, accesible sin
 * sesión por cualquiera que posea el token. La contraseña (si el enlace la
 * tiene) va siempre en la cabecera X-Share-Password, nunca en la URL, así
 * que browse/download/upload la reciben como parámetro explícito en vez de
 * leerla de un cierre potencialmente desactualizado justo tras desbloquear.
 */
export default function PublicSharePage() {
  const { token = '' } = useParams<{ token: string }>()
  const [info, setInfo] = useState<PublicShareInfo | null>(null)
  const [password, setPassword] = useState('')
  const [passwordInput, setPasswordInput] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [currentPath, setCurrentPath] = useState('')
  const [browseResult, setBrowseResult] = useState<{ directories: DirectoryEntry[]; files: FileEntry[] } | null>(null)
  const [browseLoading, setBrowseLoading] = useState(false)
  const [downloadingName, setDownloadingName] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const browse = useCallback(
    async (path: string, pw: string) => {
      setBrowseLoading(true)
      setError(null)
      try {
        setBrowseResult(await api.publicShareBrowse(token, path, pw || undefined))
        setCurrentPath(path)
      } catch (err) {
        setError(err instanceof ApiClientError ? err.message : 'No se pudo abrir esta carpeta.')
      } finally {
        setBrowseLoading(false)
      }
    },
    [token],
  )

  const resolve = useCallback(
    async (pw: string) => {
      setLoading(true)
      setError(null)
      try {
        const result = await api.publicShareInfo(token, pw || undefined)
        setInfo(result)
        if (!result.requires_password) {
          setPassword(pw)
          if (result.resource_type === 'directory') {
            await browse('', pw)
          }
        }
      } catch (err) {
        setError(err instanceof ApiClientError ? err.message : 'No se pudo abrir este enlace.')
      } finally {
        setLoading(false)
      }
    },
    [token, browse],
  )

  // Solo se dispara al montar o cambiar de token -- resolve ya encadena
  // browse('') internamente cuando el recurso es una carpeta.
  useEffect(() => {
    void resolve('')
  }, [token, resolve])

  async function handleDownload(name: string, relPath: string) {
    setDownloadingName(name)
    setError(null)
    try {
      await api.downloadPublicShare(token, relPath, name, password || undefined)
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo descargar el archivo.')
    } finally {
      setDownloadingName(null)
    }
  }

  async function handleUpload(files: FileList) {
    setUploading(true)
    setError(null)
    try {
      for (const file of Array.from(files)) {
        await api.uploadToPublicShare(token, currentPath, file.name, file, password || undefined)
      }
      await browse(currentPath, password)
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo subir el archivo.')
    } finally {
      setUploading(false)
    }
  }

  const breadcrumbSegments = currentPath ? currentPath.split('/') : []

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 p-4 dark:bg-slate-950">
      <div className="w-full max-w-xl rounded-lg border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h1 className="mb-4 text-lg font-semibold text-slate-900 dark:text-slate-50">NexusCloud — Enlace compartido</h1>

        {loading ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : !info ? (
          <p className="text-sm text-red-600 dark:text-red-400">{error ?? 'Enlace no disponible.'}</p>
        ) : info.revoked ? (
          <p className="text-sm text-red-600 dark:text-red-400">Este enlace ha sido revocado.</p>
        ) : info.expired ? (
          <p className="text-sm text-red-600 dark:text-red-400">Este enlace ha expirado.</p>
        ) : info.exhausted ? (
          <p className="text-sm text-red-600 dark:text-red-400">Este enlace alcanzó su límite de descargas.</p>
        ) : info.requires_password ? (
          <div className="space-y-2">
            <p className="text-sm text-slate-600 dark:text-slate-400">Este enlace está protegido con contraseña.</p>
            {info.password_incorrect && <p className="text-sm text-red-600 dark:text-red-400">Contraseña incorrecta.</p>}
            <input
              type="password"
              autoFocus
              value={passwordInput}
              onChange={(e) => setPasswordInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && void resolve(passwordInput)}
              placeholder="Contraseña"
              className="w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            />
            <button
              onClick={() => void resolve(passwordInput)}
              className="w-full rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700"
            >
              Acceder
            </button>
          </div>
        ) : info.resource_type === 'file' ? (
          <div>
            <p className="text-sm font-medium text-slate-800 dark:text-slate-200">{info.name}</p>
            <p className="mb-3 text-sm text-slate-500 dark:text-slate-400">{formatBytes(info.size_bytes ?? 0)}</p>
            {info.can_download && (
              <button
                disabled={downloadingName !== null}
                onClick={() => void handleDownload(info.name ?? 'archivo', '')}
                className="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              >
                {downloadingName ? 'Descargando…' : 'Descargar'}
              </button>
            )}
          </div>
        ) : (
          <div>
            <div className="mb-3 flex flex-wrap items-center gap-1 text-sm text-slate-500 dark:text-slate-400">
              <button onClick={() => void browse('', password)} className="hover:text-blue-700 dark:hover:text-blue-400">
                {info.name}
              </button>
              {breadcrumbSegments.map((seg, i) => (
                <span key={i} className="flex items-center gap-1">
                  <span>/</span>
                  <button
                    onClick={() => void browse(breadcrumbSegments.slice(0, i + 1).join('/'), password)}
                    className="hover:text-blue-700 dark:hover:text-blue-400"
                  >
                    {seg}
                  </button>
                </span>
              ))}
            </div>

            {info.can_upload && (
              <div className="mb-3">
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  className="hidden"
                  onChange={(e) => {
                    if (e.target.files?.length) void handleUpload(e.target.files)
                    e.target.value = ''
                  }}
                />
                <button
                  disabled={uploading}
                  onClick={() => fileInputRef.current?.click()}
                  className="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  {uploading ? 'Subiendo…' : 'Subir archivo'}
                </button>
              </div>
            )}

            {browseLoading ? (
              <p className="text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
            ) : !browseResult || (browseResult.directories.length === 0 && browseResult.files.length === 0) ? (
              <p className="text-sm text-slate-500 dark:text-slate-400">Esta carpeta está vacía.</p>
            ) : (
              <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                {browseResult.directories.map((d) => (
                  <li key={d.id} className="flex items-center justify-between py-2 text-sm">
                    <button
                      onClick={() => void browse(currentPath ? `${currentPath}/${d.name}` : d.name, password)}
                      className="flex items-center gap-2 font-medium text-slate-800 hover:text-blue-700 dark:text-slate-200 dark:hover:text-blue-400"
                    >
                      <span aria-hidden>📁</span>
                      {d.name}
                    </button>
                  </li>
                ))}
                {browseResult.files.map((f) => (
                  <li key={f.id} className="flex items-center justify-between py-2 text-sm">
                    <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                      <span aria-hidden>📄</span>
                      {f.name}
                      <span className="text-xs text-slate-400">{formatBytes(f.size_bytes)}</span>
                    </span>
                    {info.can_download && (
                      <button
                        disabled={downloadingName !== null}
                        onClick={() => void handleDownload(f.name, currentPath ? `${currentPath}/${f.name}` : f.name)}
                        className="rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-950"
                      >
                        {downloadingName === f.name ? 'Descargando…' : 'Descargar'}
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        {error && info && !info.requires_password && (
          <div className="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
            {error}
          </div>
        )}
      </div>
    </div>
  )
}
