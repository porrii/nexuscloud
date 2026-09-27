import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type ActivityEvent, type ListResult } from '../api/client'

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

const relativeTimeFormatter = new Intl.RelativeTimeFormat('es', { numeric: 'auto' })

// formatRelativeTime da "hace 5 minutos" / "hace 2 horas" / "hace 3 días" a
// partir de una fecha ISO -- Intl.RelativeTimeFormat es nativo del
// navegador, sin dependencia nueva.
function formatRelativeTime(iso: string): string {
  const diffMs = new Date(iso).getTime() - Date.now()
  const diffMin = Math.round(diffMs / 60000)
  if (Math.abs(diffMin) < 1) return 'justo ahora'
  if (Math.abs(diffMin) < 60) return relativeTimeFormatter.format(diffMin, 'minute')
  const diffHour = Math.round(diffMin / 60)
  if (Math.abs(diffHour) < 24) return relativeTimeFormatter.format(diffHour, 'hour')
  return relativeTimeFormatter.format(Math.round(diffHour / 24), 'day')
}

// describeActivity construye "verbo + objeto" (§88: "Ivan subió:
// documento.pdf") a partir de event_type + metadata -- ver
// recentActivityEventTypes en internal/api/v1/activity_handlers.go para la
// lista exacta de tipos que puede llegar aquí. download/delete/share_revoke
// no siempre traen un nombre (metadata puede venir vacía en esos handlers
// hoy): se cae a una descripción genérica en vez de mostrar "undefined".
function describeActivity(e: ActivityEvent): string {
  const name = typeof e.metadata?.name === 'string' ? e.metadata.name : undefined
  switch (e.event_type) {
    case 'upload':
      return `subió ${name ?? 'un archivo'}`
    case 'download':
      return `descargó ${name ?? 'un archivo'}`
    case 'delete':
      return `eliminó ${name ?? (e.target_type === 'directory' ? 'una carpeta' : 'un archivo')}`
    case 'move':
      return `movió ${name ?? 'un elemento'}`
    case 'share_create': {
      const shareType = typeof e.metadata?.share_type === 'string' ? e.metadata.share_type : undefined
      if (shareType === 'user') return 'compartió algo con un usuario'
      if (shareType === 'group') return 'compartió algo con un grupo'
      if (shareType === 'link') return 'creó un enlace de compartición'
      return 'compartió algo'
    }
    case 'share_revoke':
      return 'revocó una compartición'
    default:
      return e.event_type
  }
}

type Tab = 'favorites' | 'recent'

/**
 * Favoritos y Recientes (§87, §88, §143): una sola página con dos pestañas,
 * mismo patrón que "Compartido conmigo"/"Compartido por mí" ya unificados
 * en SharedPage en vez de dos entradas de navegación separadas.
 */
export default function FavoritesPage() {
  const [tab, setTab] = useState<Tab>('favorites')

  const [favorites, setFavorites] = useState<ListResult | null>(null)
  const [favoritesError, setFavoritesError] = useState<string | null>(null)

  const [activity, setActivity] = useState<ActivityEvent[] | null>(null)
  const [activityError, setActivityError] = useState<string | null>(null)

  const loadFavorites = useCallback(async () => {
    try {
      setFavorites(await api.favorites())
    } catch (err) {
      setFavoritesError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar los favoritos.')
    }
  }, [])

  const loadActivity = useCallback(async () => {
    try {
      setActivity(await api.activity())
    } catch (err) {
      setActivityError(err instanceof ApiClientError ? err.message : 'No se pudo cargar la actividad reciente.')
    }
  }, [])

  useEffect(() => {
    if (tab === 'favorites') void loadFavorites()
    else void loadActivity()
  }, [tab, loadFavorites, loadActivity])

  async function handleRemoveFavorite(id: string) {
    try {
      await api.removeFavorite(id)
      await loadFavorites()
    } catch (err) {
      setFavoritesError(err instanceof ApiClientError ? err.message : 'No se pudo quitar de favoritos.')
    }
  }

  return (
    <div className="flex h-full flex-col p-6">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-50">Favoritos</h1>
        <div className="flex gap-1 rounded-md bg-slate-100 p-1 dark:bg-slate-800">
          <button
            onClick={() => setTab('favorites')}
            className={`rounded px-3 py-1 text-sm font-medium ${
              tab === 'favorites' ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-500 dark:text-slate-400'
            }`}
          >
            Favoritos
          </button>
          <button
            onClick={() => setTab('recent')}
            className={`rounded px-3 py-1 text-sm font-medium ${
              tab === 'recent' ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-500 dark:text-slate-400'
            }`}
          >
            Recientes
          </button>
        </div>
      </div>

      {tab === 'favorites' ? (
        <div className="flex-1 overflow-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
          {favoritesError && (
            <div className="m-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
              {favoritesError}
            </div>
          )}
          {!favorites ? (
            <p className="p-6 text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
          ) : favorites.directories.length === 0 && favorites.files.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-1 p-12 text-center">
              <p className="text-sm font-medium text-slate-600 dark:text-slate-300">Todavía no tienes ningún favorito</p>
              <p className="text-sm text-slate-400 dark:text-slate-500">Márcalos con ☆ desde "Mis archivos"</p>
            </div>
          ) : (
            <ul className="divide-y divide-slate-100 dark:divide-slate-800">
              {favorites.directories.map((d) => (
                <li key={d.id} className="flex items-center justify-between px-4 py-3 text-sm">
                  <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                    <span aria-hidden>📁</span>
                    {d.name}
                  </span>
                  {d.favorite_id && (
                    <button
                      onClick={() => void handleRemoveFavorite(d.favorite_id!)}
                      className="rounded px-2 py-1 text-xs text-amber-600 hover:bg-amber-50 dark:text-amber-400 dark:hover:bg-amber-950"
                    >
                      ★ Quitar de favoritos
                    </button>
                  )}
                </li>
              ))}
              {favorites.files.map((f) => (
                <li key={f.id} className="flex items-center justify-between px-4 py-3 text-sm">
                  <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                    <span aria-hidden>📄</span>
                    {f.name}
                    <span className="text-xs text-slate-400">{formatBytes(f.size_bytes)}</span>
                  </span>
                  {f.favorite_id && (
                    <button
                      onClick={() => void handleRemoveFavorite(f.favorite_id!)}
                      className="rounded px-2 py-1 text-xs text-amber-600 hover:bg-amber-50 dark:text-amber-400 dark:hover:bg-amber-950"
                    >
                      ★ Quitar de favoritos
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      ) : (
        <div className="flex-1 overflow-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
          {activityError && (
            <div className="m-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
              {activityError}
            </div>
          )}
          {!activity ? (
            <p className="p-6 text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
          ) : activity.length === 0 ? (
            <p className="p-12 text-center text-sm text-slate-500 dark:text-slate-400">Todavía no hay actividad reciente.</p>
          ) : (
            <ul className="divide-y divide-slate-100 dark:divide-slate-800">
              {activity.map((e) => (
                <li key={e.id} className="px-4 py-3 text-sm text-slate-700 dark:text-slate-300">
                  Tú {describeActivity(e)} <span className="text-slate-400 dark:text-slate-500">· {formatRelativeTime(e.occurred_at)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
