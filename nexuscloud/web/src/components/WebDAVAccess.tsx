import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { api, ApiClientError, type CreatedWebDAVToken, type WebDAVToken } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import ConfirmDialog from './ConfirmDialog'

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
}

// WebDAV autentica con HTTP Basic (ADR-034): el token viaja en cada petición,
// así que por HTTP plano iría sin cifrar. En loopback no importa (desarrollo).
function isInsecureRemote(): boolean {
  const { protocol, hostname } = window.location
  return protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(hostname)
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
function CreatedTokenPanel({ created, username, onDismiss }: { created: CreatedWebDAVToken; username: string; onDismiss: () => void }) {
  return (
    <div
      role="status"
      className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-950/40"
    >
      <p className="mb-3 text-sm font-medium text-amber-900 dark:text-amber-200">
        Acceso «{created.label}» creado. Copia el token ahora: no volverá a mostrarse.
      </p>
      <div className="space-y-3">
        <CopyField label="Dirección del servidor" value={`${window.location.origin}${created.webdav_path}/`} />
        <CopyField label="Usuario" value={username} />
        <CopyField label="Contraseña (el token)" value={created.token} />
      </div>
      {isInsecureRemote() && (
        <p className="mt-3 text-xs text-amber-800 dark:text-amber-300">
          Esta dirección usa HTTP: el token viajaría sin cifrar en cada petición. Configura HTTPS (por ejemplo, un proxy
          inverso con TLS) antes de usarlo fuera de una red de confianza.
        </p>
      )}
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
 * Gestión de los tokens de acceso WebDAV del usuario (§43, ADR-034). Es la
 * "contraseña" que usan los clientes WebDAV; nunca se usa la de la cuenta,
 * porque saltaría el segundo factor (TOTP/passkey).
 *
 * Se oculta sola si el servidor no ofrece WebDAV (las rutas responden 404
 * con webdav.enabled=false, el valor por defecto).
 */
export default function WebDAVAccess() {
  const { user } = useAuth()
  // null = todavía no sabemos: no se pinta nada hasta la primera respuesta,
  // para no hacer parpadear la sección en las instancias sin WebDAV.
  const [supported, setSupported] = useState<boolean | null>(null)
  const [tokens, setTokens] = useState<WebDAVToken[]>([])
  const [error, setError] = useState<string | null>(null)
  const [newLabel, setNewLabel] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<CreatedWebDAVToken | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<WebDAVToken | null>(null)

  const load = useCallback(async () => {
    try {
      setTokens(await api.listWebDAVTokens())
      setSupported(true)
    } catch (err) {
      if (err instanceof ApiClientError && err.status === 404) {
        setSupported(false)
        return
      }
      setSupported(true)
      setError(err instanceof ApiClientError ? err.message : 'No se pudieron cargar los accesos WebDAV.')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function handleCreate() {
    setError(null)
    setCreating(true)
    try {
      // Sin nombre, el servidor pone "WebDAV" por defecto.
      setCreated(await api.createWebDAVToken((newLabel ?? '').trim()))
      setNewLabel(null)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo crear el acceso.')
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke() {
    if (!pendingRevoke) return
    const target = pendingRevoke
    setPendingRevoke(null)
    try {
      await api.revokeWebDAVToken(target.id)
      // Si justo se estaba mostrando este token, ya no sirve: se retira.
      if (created?.id === target.id) setCreated(null)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo revocar el acceso.')
    }
  }

  if (!supported) return null

  return (
    <>
      <section className="mb-8 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="mb-1 flex items-center justify-between">
          <h2 className="text-sm font-medium text-slate-500 dark:text-slate-400">Acceso WebDAV</h2>
          {newLabel === null && (
            <button
              onClick={() => setNewLabel('')}
              className="rounded px-2 py-1 text-xs font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
            >
              Crear acceso
            </button>
          )}
        </div>
        <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">
          Monta tus archivos como unidad de red o sincronízalos con clientes WebDAV (Explorador de Windows, Finder, rclone,
          Cyberduck…). Cada dispositivo usa su propio token en lugar de tu contraseña y puedes revocarlo cuando quieras.
        </p>

        {error && <p className="mb-3 text-sm text-red-600 dark:text-red-400">{error}</p>}

        {newLabel !== null && (
          <div className="mb-4 flex items-center gap-2">
            <input
              autoFocus
              aria-label="Nombre del acceso"
              placeholder='Nombre, p.ej. "portátil de casa"'
              maxLength={100}
              value={newLabel}
              onChange={(e) => setNewLabel(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !creating) void handleCreate()
                if (e.key === 'Escape' && !creating) setNewLabel(null)
              }}
              className="flex-1 rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
            />
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

        {created && <CreatedTokenPanel created={created} username={user?.username ?? ''} onDismiss={() => setCreated(null)} />}

        {tokens.length === 0 ? (
          <p className="text-sm text-slate-500 dark:text-slate-400">Todavía no has creado ningún acceso WebDAV.</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {tokens.map((t) => (
              <li key={t.id} className="flex items-center justify-between py-3 text-sm">
                <div>
                  <p className="text-slate-800 dark:text-slate-200">{t.label}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    creado {formatDate(t.created_at)} · último uso {t.last_used_at ? formatDate(t.last_used_at) : 'nunca'}
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
          title="Revocar acceso WebDAV"
          message={`"${pendingRevoke.label}" dejará de funcionar de inmediato: los dispositivos que lo usen tendrán que configurarse con un token nuevo.`}
          confirmLabel="Revocar"
          danger
          onConfirm={() => void handleRevoke()}
          onCancel={() => setPendingRevoke(null)}
        />
      )}
    </>
  )
}
