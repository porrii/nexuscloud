import { useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, ApiClientError, type AnonymousUploadInfo } from '../api/client'

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

type UploadState = { name: string; status: 'subiendo' | 'ok' | 'error'; message?: string }

/**
 * Landing de un enlace de subida anónima (§38, ADR-039) -- fuera de
 * RequireAuth, accesible sin sesión por cualquiera que posea el token. A
 * diferencia de PublicSharePage, no hay contraseña ni navegación: el único
 * gesto posible es subir un archivo a la raíz de la carpeta del enlace, así
 * que no hay nada que listar ni ningún breadcrumb.
 */
export default function AnonymousUploadPage() {
  const { token = '' } = useParams<{ token: string }>()
  const [info, setInfo] = useState<AnonymousUploadInfo | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [loading, setLoading] = useState(true)
  const [uploads, setUploads] = useState<UploadState[]>([])
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setNotFound(false)
    api
      .getAnonymousUpload(token)
      .then((result) => {
        if (!cancelled) setInfo(result)
      })
      .catch(() => {
        if (!cancelled) setNotFound(true)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [token])

  async function handleUpload(files: FileList) {
    for (const file of Array.from(files)) {
      setUploads((prev) => [...prev, { name: file.name, status: 'subiendo' }])
      try {
        await api.uploadToAnonymousUpload(token, file.name, file)
        setUploads((prev) => prev.map((u) => (u.name === file.name && u.status === 'subiendo' ? { ...u, status: 'ok' } : u)))
      } catch (err) {
        const message = err instanceof ApiClientError ? err.message : 'No se pudo subir el archivo.'
        setUploads((prev) => prev.map((u) => (u.name === file.name && u.status === 'subiendo' ? { ...u, status: 'error', message } : u)))
      }
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 p-4 dark:bg-slate-950">
      <div className="w-full max-w-xl rounded-lg border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <h1 className="mb-4 text-lg font-semibold text-slate-900 dark:text-slate-50">NexusCloud — Subir un archivo</h1>

        {loading ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : notFound || !info ? (
          <p className="text-sm text-red-600 dark:text-red-400">Enlace no encontrado.</p>
        ) : (
          <div>
            {info.label && <p className="mb-1 text-sm font-medium text-slate-800 dark:text-slate-200">{info.label}</p>}
            {info.max_upload_size_bytes !== undefined && (
              <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">Límite por archivo: {formatBytes(info.max_upload_size_bytes)}</p>
            )}
            <p className="mb-3 text-sm text-slate-600 dark:text-slate-400">
              Puedes subir uno o varios archivos. Quien te dio este enlace no puede verte a ti, solo recibirá lo que subas.
            </p>

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
              onClick={() => fileInputRef.current?.click()}
              className="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700"
            >
              Elegir archivos…
            </button>

            {uploads.length > 0 && (
              <ul className="mt-4 space-y-1">
                {uploads.map((u, i) => (
                  <li key={i} className="flex items-center justify-between text-sm">
                    <span className="truncate text-slate-700 dark:text-slate-300">{u.name}</span>
                    <span
                      className={
                        u.status === 'ok'
                          ? 'text-emerald-600 dark:text-emerald-400'
                          : u.status === 'error'
                            ? 'text-red-600 dark:text-red-400'
                            : 'text-slate-400'
                      }
                    >
                      {u.status === 'ok' ? 'Subido' : u.status === 'error' ? (u.message ?? 'Error') : 'Subiendo…'}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
