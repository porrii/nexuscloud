import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type Session, type WebAuthnCredential } from '../api/client'
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

  const [credentials, setCredentials] = useState<WebAuthnCredential[]>([])
  const [webauthnSupported, setWebauthnSupported] = useState(true)
  const [passkeyError, setPasskeyError] = useState<string | null>(null)
  const [addingPasskey, setAddingPasskey] = useState(false)
  const [newPasskeyLabel, setNewPasskeyLabel] = useState<string | null>(null)
  const [pendingRevokeCredential, setPendingRevokeCredential] = useState<WebAuthnCredential | null>(null)

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

  const loadCredentials = useCallback(async () => {
    try {
      setCredentials(await api.listWebAuthnCredentials())
    } catch (err) {
      // 404 = security.webAuthn.enabled=false en el servidor (§25): no es
      // un error del usuario, simplemente esta instancia no lo ofrece.
      if (err instanceof ApiClientError && err.status === 404) {
        setWebauthnSupported(false)
        return
      }
      setPasskeyError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar los passkeys.')
    }
  }, [])

  useEffect(() => {
    void load()
    void loadCredentials()
  }, [load, loadCredentials])

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

  async function handleAddPasskey() {
    setPasskeyError(null)
    setAddingPasskey(true)
    try {
      await api.registerWebAuthnCredential(newPasskeyLabel?.trim() || 'Passkey')
      setNewPasskeyLabel(null)
      await loadCredentials()
    } catch (err) {
      setPasskeyError(err instanceof ApiClientError ? err.message : 'No se pudo registrar el passkey.')
    } finally {
      setAddingPasskey(false)
    }
  }

  async function handleRevokeCredential() {
    if (!pendingRevokeCredential) return
    try {
      await api.revokeWebAuthnCredential(pendingRevokeCredential.id)
      setPendingRevokeCredential(null)
      await loadCredentials()
    } catch (err) {
      setPendingRevokeCredential(null)
      setPasskeyError(err instanceof ApiClientError ? err.message : 'No se pudo revocar el passkey.')
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

      {webauthnSupported && (
        <section className="mb-8 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-500 dark:text-slate-400">Passkeys</h2>
            {newPasskeyLabel === null && (
              <button
                onClick={() => setNewPasskeyLabel('')}
                className="rounded px-2 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
              >
                Añadir passkey
              </button>
            )}
          </div>

          {passkeyError && <p className="mb-3 text-sm text-red-600 dark:text-red-400">{passkeyError}</p>}

          {newPasskeyLabel !== null && (
            <div className="mb-4 flex items-center gap-2">
              <input
                autoFocus
                placeholder='Nombre, p.ej. "portátil de trabajo"'
                value={newPasskeyLabel}
                onChange={(e) => setNewPasskeyLabel(e.target.value)}
                className="flex-1 rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
              />
              <button
                onClick={() => void handleAddPasskey()}
                disabled={addingPasskey}
                className="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:opacity-60"
              >
                {addingPasskey ? 'Esperando…' : 'Continuar'}
              </button>
              <button
                onClick={() => setNewPasskeyLabel(null)}
                disabled={addingPasskey}
                className="rounded-md px-3 py-2 text-sm text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                Cancelar
              </button>
            </div>
          )}

          {credentials.length === 0 ? (
            <p className="text-sm text-slate-500 dark:text-slate-400">No tienes ningún passkey registrado todavía.</p>
          ) : (
            <ul className="divide-y divide-slate-100 dark:divide-slate-800">
              {credentials.map((c) => (
                <li key={c.id} className="flex items-center justify-between py-3 text-sm">
                  <div>
                    <p className="text-slate-800 dark:text-slate-200">{c.label}</p>
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                      añadido {formatDate(c.created_at)} · último uso {c.last_used_at ? formatDate(c.last_used_at) : 'nunca'}
                    </p>
                  </div>
                  <button
                    onClick={() => setPendingRevokeCredential(c)}
                    className="rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
                  >
                    Revocar
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

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

      {pendingRevokeCredential && (
        <ConfirmDialog
          title="Revocar passkey"
          message={`"${pendingRevokeCredential.label}" ya no podrá usarse para iniciar sesión.`}
          confirmLabel="Revocar"
          danger
          onConfirm={() => void handleRevokeCredential()}
          onCancel={() => setPendingRevokeCredential(null)}
        />
      )}
    </div>
  )
}
