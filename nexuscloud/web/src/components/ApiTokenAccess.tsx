import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { api, ApiClientError, type ApiToken, type CreatedApiToken } from '../api/client'
import ConfirmDialog from './ConfirmDialog'

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
}

// EXPIRY_OPTIONS son los mismos plazos discretos que ya ofrece la CLI
// (--expires-in 30d/90d/1y/never, api_token_cmd.go): sin selector de fecha
// libre, para no complicar la UI de algo que la mayoría de la gente deja en
// "nunca". El valor se calcula aquí (fecha absoluta ISO), igual que ya hace
// el cliente Flutter/web al crear un enlace de compartición con expiración.
const EXPIRY_OPTIONS = [
  { value: '', label: 'Nunca' },
  { value: '30d', label: '30 días' },
  { value: '90d', label: '90 días' },
  { value: '1y', label: '1 año' },
] as const

function expiryToIsoDate(value: string): string | undefined {
  if (value === '') return undefined
  const days = value === '1y' ? 365 : parseInt(value, 10)
  return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString()
}

function CopyField({ label, value }: { label: string; value: string }) {
  const id = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(false), 2000)
    return () => window.clearTimeout(timer)
  }, [copied])

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      // Sin contexto seguro (HTTP en una LAN) el navegador no expone el
      // portapapeles: se deja el texto seleccionado para copiarlo con Ctrl+C.
      inputRef.current?.select()
    }
  }

  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-300">
        {label}
      </label>
      <div className="flex items-center gap-2">
        <input
          id={id}
          ref={inputRef}
          readOnly
          value={value}
          onFocus={(e) => e.currentTarget.select()}
          className="min-w-0 flex-1 rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
        />
        <button
          onClick={() => void copy()}
          className="w-20 rounded-md border border-slate-300 px-2 py-2 text-xs font-medium text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
        >
          {copied ? 'Copiado' : 'Copiar'}
        </button>
      </div>
    </div>
  )
}

// CreatedTokenPanel muestra el secreto una única vez (§78): en cuanto se
// descarta, ni el servidor ni esta página pueden volver a enseñarlo.
function CreatedTokenPanel({ created, onDismiss }: { created: CreatedApiToken; onDismiss: () => void }) {
  return (
    <div
      role="status"
      className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-950/40"
    >
      <p className="mb-3 text-sm font-medium text-amber-900 dark:text-amber-200">
        Token «{created.label}» creado. Cópialo ahora: no volverá a mostrarse.
      </p>
      <CopyField label="Token" value={created.token} />
      <p className="mt-3 text-xs text-amber-800 dark:text-amber-300">
        Úsalo como cabecera <code>Authorization: Bearer {'<token>'}</code> contra cualquier endpoint de la API. Actúa
        exactamente como tu cuenta: no lo compartas.
        {created.expires_at ? ` Caduca el ${formatDate(created.expires_at)}.` : ' No caduca.'}
      </p>
      <div className="mt-3 flex justify-end">
        <button
          onClick={onDismiss}
          className="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white transition hover:bg-blue-700"
        >
          Ya lo he copiado
        </button>
      </div>
    </div>
  )
}

/**
 * Gestión de los tokens de acceso a la API del usuario (§78, ADR-037):
 * autentican peticiones Bearer contra la API REST completa, de alcance
 * todo-o-nada (actúan exactamente como el usuario) -- para scripts e
 * integraciones que no deben volver a pasar por el navegador cada vez que
 * expira una sesión normal. A diferencia del acceso WebDAV, siempre están
 * disponibles (ninguna opción de config los desactiva).
 */
export default function ApiTokenAccess() {
  const [tokens, setTokens] = useState<ApiToken[]>([])
  const [error, setError] = useState<string | null>(null)
  const [newLabel, setNewLabel] = useState<string | null>(null)
  const [newExpiry, setNewExpiry] = useState<string>('')
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<CreatedApiToken | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<ApiToken | null>(null)

  const load = useCallback(async () => {
    try {
      setTokens(await api.apiTokens())
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar los tokens de API.')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function handleCreate() {
    setError(null)
    setCreating(true)
    try {
      // Sin nombre, el servidor pone "Token de API" por defecto.
      setCreated(await api.createApiToken((newLabel ?? '').trim(), expiryToIsoDate(newExpiry)))
      setNewLabel(null)
      setNewExpiry('')
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo crear el token.')
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke() {
    if (!pendingRevoke) return
    const target = pendingRevoke
    setPendingRevoke(null)
    try {
      await api.revokeApiToken(target.id)
      // Si justo se estaba mostrando este token, ya no sirve: se retira.
      if (created?.id === target.id) setCreated(null)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo revocar el token.')
    }
  }

  return (
    <>
      <section className="mb-8 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="mb-1 flex items-center justify-between">
          <h2 className="text-sm font-medium text-slate-500 dark:text-slate-400">Tokens de API</h2>
          {newLabel === null && (
            <button
              onClick={() => setNewLabel('')}
              className="rounded px-2 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
            >
              Crear token
            </button>
          )}
        </div>
        <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">
          Para scripts e integraciones que hablan con la API sin pasar por el navegador. Cada token actúa exactamente
          como tu cuenta: créalo con un nombre que lo identifique y revócalo cuando ya no lo necesites.
        </p>

        {error && <p className="mb-3 text-sm text-red-600 dark:text-red-400">{error}</p>}

        {newLabel !== null && (
          <div className="mb-4 flex items-center gap-2">
            <input
              autoFocus
              aria-label="Nombre del token"
              placeholder='Nombre, p.ej. "script de backup"'
              maxLength={100}
              value={newLabel}
              onChange={(e) => setNewLabel(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !creating) void handleCreate()
                if (e.key === 'Escape' && !creating) setNewLabel(null)
              }}
              className="flex-1 rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            />
            <select
              aria-label="Expiración"
              value={newExpiry}
              onChange={(e) => setNewExpiry(e.target.value)}
              className="rounded-md border border-slate-300 px-2 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            >
              {EXPIRY_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
            <button
              onClick={() => void handleCreate()}
              disabled={creating}
              className="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:opacity-60"
            >
              {creating ? 'Creando…' : 'Crear'}
            </button>
            <button
              onClick={() => setNewLabel(null)}
              disabled={creating}
              className="rounded-md px-3 py-2 text-sm text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
            >
              Cancelar
            </button>
          </div>
        )}

        {created && <CreatedTokenPanel created={created} onDismiss={() => setCreated(null)} />}

        {tokens.length === 0 ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Todavía no has creado ningún token de API.</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {tokens.map((t) => (
              <li key={t.id} className="flex items-center justify-between py-3 text-sm">
                <div>
                  <p className="text-slate-800 dark:text-slate-200">{t.label}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    creado {formatDate(t.created_at)} · expira {t.expires_at ? formatDate(t.expires_at) : 'nunca'} · último
                    uso {t.last_used_at ? formatDate(t.last_used_at) : 'nunca'}
                  </p>
                </div>
                <button
                  onClick={() => setPendingRevoke(t)}
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
          title="Revocar token de API"
          message={`"${pendingRevoke.label}" dejará de funcionar de inmediato: cualquier script o integración que lo use tendrá que configurarse con un token nuevo.`}
          confirmLabel="Revocar"
          danger
          onConfirm={() => void handleRevoke()}
          onCancel={() => setPendingRevoke(null)}
        />
      )}
    </>
  )
}
