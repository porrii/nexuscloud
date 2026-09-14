import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type CreateShareInput, type Group, type Share } from '../api/client'

interface ShareDialogProps {
  resource: { id: string; name: string; isDirectory: boolean }
  onClose: () => void
}

type ShareTab = 'user' | 'group' | 'link'

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
}

function shareTargetLabel(s: Share): string {
  if (s.share_type === 'user') return `Usuario: ${s.target_username ?? s.target_user_id}`
  if (s.share_type === 'group') return `Grupo: ${s.target_group_name ?? s.target_group_id}`
  return s.label ? `Enlace: ${s.label}` : 'Enlace público'
}

/**
 * Compartir un archivo o carpeta (§37): usuario, grupo o enlace público, más
 * la lista de comparticiones activas de ESE recurso con opción de revocar.
 * Modal en vez de página propia -- igual criterio que VersionHistoryDialog.
 */
export default function ShareDialog({ resource, onClose }: ShareDialogProps) {
  const [tab, setTab] = useState<ShareTab>('user')
  const [shares, setShares] = useState<Share[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [newToken, setNewToken] = useState<string | null>(null)

  const [username, setUsername] = useState('')
  const [groupId, setGroupId] = useState('')
  const [label, setLabel] = useState('')
  const [canUpload, setCanUpload] = useState(false)
  const [password, setPassword] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [maxDownloads, setMaxDownloads] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [allShares, allGroups] = await Promise.all([api.listShares('by-me'), api.listGroups()])
      setShares(allShares.filter((s) => s.resource_id === resource.id))
      setGroups(allGroups)
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo cargar la información de compartición.')
    } finally {
      setLoading(false)
    }
  }, [resource.id])

  useEffect(() => {
    void load()
  }, [load])

  async function handleCreate() {
    setError(null)
    setNewToken(null)
    const input: CreateShareInput = {
      resource_type: resource.isDirectory ? 'directory' : 'file',
      resource_id: resource.id,
      share_type: tab,
      label: tab === 'link' && label ? label : undefined,
      can_download: true,
      can_upload: tab === 'link' ? canUpload : false,
      password: tab === 'link' && password ? password : undefined,
      expires_at: tab === 'link' && expiresAt ? new Date(expiresAt).toISOString() : undefined,
      max_downloads: tab === 'link' && maxDownloads ? Number(maxDownloads) : undefined,
    }
    if (tab === 'user') input.target_username = username.trim()
    if (tab === 'group') input.target_group_id = groupId

    setCreating(true)
    try {
      const created = await api.createShare(input)
      if (created.token) setNewToken(created.token)
      setUsername('')
      setGroupId('')
      setLabel('')
      setPassword('')
      setExpiresAt('')
      setMaxDownloads('')
      setCanUpload(false)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo crear la compartición.')
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(id: string) {
    setError(null)
    try {
      await api.revokeShare(id)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo revocar la compartición.')
    }
  }

  function publicLinkUrl(token: string): string {
    return `${window.location.origin}/s/${token}`
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="max-h-[85vh] w-full max-w-lg overflow-auto rounded-lg bg-white p-5 shadow-lg dark:bg-slate-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-start justify-between">
          <div>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-50">Compartir</h2>
            <p className="text-sm text-slate-500 dark:text-slate-400">{resource.name}</p>
          </div>
          <button onClick={onClose} className="text-sm text-slate-500 hover:text-slate-700 dark:hover:text-slate-300">
            Cerrar
          </button>
        </div>

        {error && (
          <div className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
            {error}
          </div>
        )}
        {newToken && (
          <div className="mb-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300">
            <p className="mb-1 font-medium">Enlace creado — guárdalo ahora, no se volverá a mostrar:</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 truncate rounded bg-white/60 px-2 py-1 text-xs dark:bg-black/20">{publicLinkUrl(newToken)}</code>
              <button
                onClick={() => void navigator.clipboard.writeText(publicLinkUrl(newToken))}
                className="rounded px-2 py-1 text-xs font-medium hover:bg-emerald-100 dark:hover:bg-emerald-900"
              >
                Copiar
              </button>
            </div>
          </div>
        )}

        <div className="mb-3 flex gap-1 rounded-md bg-slate-100 p-1 dark:bg-slate-800">
          {(['user', 'group', 'link'] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`flex-1 rounded px-2 py-1 text-sm font-medium ${
                tab === t ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-500 dark:text-slate-400'
              }`}
            >
              {t === 'user' ? 'Usuario' : t === 'group' ? 'Grupo' : 'Enlace'}
            </button>
          ))}
        </div>

        <div className="mb-4 space-y-2 rounded-md border border-slate-200 p-3 dark:border-slate-800">
          {tab === 'user' && (
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="Nombre de usuario"
              className="w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            />
          )}
          {tab === 'group' && (
            <select
              value={groupId}
              onChange={(e) => setGroupId(e.target.value)}
              className="w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            >
              <option value="">Selecciona un grupo…</option>
              {groups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name}
                </option>
              ))}
            </select>
          )}
          {tab === 'link' && (
            <>
              <input
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder="Nombre del enlace (opcional)"
                className="w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
              />
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Contraseña (opcional)"
                className="w-full rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
              />
              <div className="flex gap-2">
                <input
                  type="datetime-local"
                  value={expiresAt}
                  onChange={(e) => setExpiresAt(e.target.value)}
                  title="Fecha de expiración (opcional)"
                  className="flex-1 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
                />
                <input
                  type="number"
                  min={1}
                  value={maxDownloads}
                  onChange={(e) => setMaxDownloads(e.target.value)}
                  placeholder="Límite descargas"
                  className="w-32 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
                />
              </div>
              {resource.isDirectory && (
                <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
                  <input type="checkbox" checked={canUpload} onChange={(e) => setCanUpload(e.target.checked)} />
                  Permitir subir archivos a esta carpeta
                </label>
              )}
            </>
          )}
          <button
            onClick={() => void handleCreate()}
            disabled={creating || (tab === 'user' && !username.trim()) || (tab === 'group' && !groupId)}
            className="w-full rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          >
            {creating ? 'Creando…' : tab === 'link' ? 'Crear enlace' : 'Compartir'}
          </button>
        </div>

        <h3 className="mb-2 text-sm font-medium text-slate-700 dark:text-slate-300">Comparticiones activas</h3>
        {loading ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : shares.length === 0 ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Nadie tiene acceso todavía.</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {shares.map((s) => (
              <li key={s.id} className="flex items-center justify-between py-2 text-sm">
                <div>
                  <p className="text-slate-800 dark:text-slate-200">{shareTargetLabel(s)}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {s.can_upload ? 'Descarga y subida' : 'Solo descarga'}
                    {s.has_password ? ' · con contraseña' : ''}
                    {s.expires_at ? ` · expira ${formatDate(s.expires_at)}` : ''}
                    {s.max_downloads ? ` · ${s.download_count}/${s.max_downloads} descargas` : ''}
                  </p>
                </div>
                <button
                  onClick={() => void handleRevoke(s.id)}
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
