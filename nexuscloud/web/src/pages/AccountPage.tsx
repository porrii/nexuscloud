import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type Session } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog'
import { useAuth } from '../auth/AuthContext'

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
}

export default function AccountPage() {
  const { user } = useAuth()
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<Session | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setSessions(await api.sessions())
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar las sesiones.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function handleRevoke() {
    if (!pendingRevoke) return
    try {
      await api.revokeSession(pendingRevoke.id)
      setPendingRevoke(null)
      await load()
    } catch (err) {
      setPendingRevoke(null)
      setError(err instanceof ApiClientError ? err.message : 'No se pudo revocar la sesión.')
    }
  }

  return (
    <div className="max-w-2xl p-6">
      <h1 className="mb-6 text-lg font-semibold text-slate-900 dark:text-slate-50">Cuenta</h1>

      <section className="mb-8 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <h2 className="mb-3 text-sm font-medium text-slate-500 dark:text-slate-400">Perfil</h2>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-slate-500 dark:text-slate-400">Usuario</dt>
          <dd className="text-slate-800 dark:text-slate-200">{user?.username}</dd>
          <dt className="text-slate-500 dark:text-slate-400">Nombre</dt>
          <dd className="text-slate-800 dark:text-slate-200">{user?.display_name}</dd>
          <dt className="text-slate-500 dark:text-slate-400">Verificación en dos pasos</dt>
          <dd className="text-slate-800 dark:text-slate-200">{user?.has_totp ? 'Activada' : 'Desactivada'}</dd>
        </dl>
      </section>

      <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <h2 className="mb-3 text-sm font-medium text-slate-500 dark:text-slate-400">Sesiones activas</h2>

        {error && <p className="mb-3 text-sm text-red-600 dark:text-red-400">{error}</p>}
        {loading ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {sessions.map((s) => (
              <li key={s.id} className="flex items-center justify-between py-3 text-sm">
                <div>
                  <p className="text-slate-800 dark:text-slate-200">{s.device || 'Dispositivo desconocido'}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {s.ip ?? 'IP desconocida'} · última actividad {formatDate(s.last_seen_at)}
                  </p>
                </div>
                <button
                  onClick={() => setPendingRevoke(s)}
                  className="rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
                >
                  Revocar
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {pendingRevoke && (
        <ConfirmDialog
          title="Revocar sesión"
          message="El dispositivo asociado a esta sesión tendrá que iniciar sesión de nuevo."
          confirmLabel="Revocar"
          danger
          onConfirm={() => void handleRevoke()}
          onCancel={() => setPendingRevoke(null)}
        />
      )}
    </div>
  )
}
