import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type AnonymousUploadLink, type CreateAnonymousUploadLinkInput } from '../api/client'

interface AnonymousUploadDialogProps {
  directory: { id: string; name: string }
  onClose: () => void
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
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

/**
 * Enlaces de subida anónima sobre una carpeta (§38, ADR-039): sin acceso al
 * resto del contenido, activados por el administrador. Modal en vez de
 * página propia, mismo criterio que ShareDialog -- pero es un modelo
 * SEPARADO de Share (sin usuario/grupo, sin contraseña, sin permiso de
 * descarga: el único gesto posible aquí es subir).
 */
export default function AnonymousUploadDialog({ directory, onClose }: AnonymousUploadDialogProps) {
  const [links, setLinks] = useState<AnonymousUploadLink[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [newToken, setNewToken] = useState<string | null>(null)

  const [label, setLabel] = useState('')
  const [maxUploadSizeMB, setMaxUploadSizeMB] = useState('')
  const [expiresAt, setExpiresAt] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const all = await api.listAnonymousUploadLinks()
      setLinks(all.filter((l) => l.directory_id === directory.id))
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar los enlaces de subida.')
    } finally {
      setLoading(false)
    }
  }, [directory.id])

  useEffect(() => {
    void load()
  }, [load])

  async function handleCreate() {
    setError(null)
    setNewToken(null)
    const input: CreateAnonymousUploadLinkInput = {
      directory_id: directory.id,
      label: label || undefined,
      max_upload_size_bytes: maxUploadSizeMB ? Math.round(Number(maxUploadSizeMB) * 1024 * 1024) : undefined,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
    }
    setCreating(true)
    try {
      const created = await api.createAnonymousUploadLink(input)
      if (created.token) setNewToken(created.token)
      setLabel('')
      setMaxUploadSizeMB('')
      setExpiresAt('')
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo crear el enlace.')
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(id: string) {
    setError(null)
    try {
      await api.revokeAnonymousUploadLink(id)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo revocar el enlace.')
    }
  }

  function anonymousUploadUrl(token: string): string {
    return `${window.location.origin}/u/${token}`
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="max-h-[85vh] w-full max-w-lg overflow-auto rounded-lg bg-white p-5 shadow-lg dark:bg-slate-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-start justify-between">
          <div>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-50">Enlace de subida anónima</h2>
            <p className="text-sm text-slate-500 dark:text-slate-400">{directory.name}</p>
          </div>
          <button onClick={onClose} className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-slate-300">
            Cerrar
          </button>
        </div>

        <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">
          Quien reciba este enlace podrá subir archivos aquí sin necesidad de cuenta, pero nunca podrá ver el resto de esta carpeta ni
          de esta instancia.
        </p>

        {error && (
          <div className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
            {error}
          </div>
        )}
        {newToken && (
          <div className="mb-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300">
            <p className="mb-1 font-medium">Enlace creado — guárdalo ahora, no se volverá a mostrar:</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 truncate rounded bg-white/60 px-2 py-1 text-xs dark:bg-black/20">{anonymousUploadUrl(newToken)}</code>
              <button
                onClick={() => void navigator.clipboard.writeText(anonymousUploadUrl(newToken))}
                className="rounded px-2 py-1 text-xs font-medium hover:bg-emerald-100 dark:hover:bg-emerald-900"
              >
                Copiar
              </button>
            </div>
          </div>
        )}

        <div className="mb-4 space-y-2 rounded-md border border-slate-200 p-3 dark:border-slate-800">
          <input
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="Nombre del enlace (opcional)"
            className="w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <div className="flex gap-2">
            <input
              type="number"
              min={1}
              value={maxUploadSizeMB}
              onChange={(e) => setMaxUploadSizeMB(e.target.value)}
              placeholder="Límite MB por archivo"
              className="flex-1 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            />
            <input
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
              title="Fecha de expiración (opcional)"
              className="flex-1 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            />
          </div>
          <button
            onClick={() => void handleCreate()}
            disabled={creating}
            className="w-full rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {creating ? 'Creando…' : 'Crear enlace'}
          </button>
        </div>

        <h3 className="mb-2 text-sm font-medium text-slate-700 dark:text-slate-300">Enlaces activos</h3>
        {loading ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : links.length === 0 ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Todavía no hay ningún enlace de subida para esta carpeta.</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {links.map((l) => (
              <li key={l.id} className="flex items-center justify-between py-2 text-sm">
                <div>
                  <p className="text-slate-800 dark:text-slate-200">{l.label || 'Sin nombre'}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {l.upload_count} archivo{l.upload_count === 1 ? '' : 's'} recibido{l.upload_count === 1 ? '' : 's'}
                    {l.max_upload_size_bytes !== undefined ? ` · límite ${formatBytes(l.max_upload_size_bytes)}` : ''}
                    {l.expires_at ? ` · expira ${formatDate(l.expires_at)}` : ''}
                  </p>
                </div>
                <button
                  onClick={() => void handleRevoke(l.id)}
                  className="rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
                >
                  Revocar
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
