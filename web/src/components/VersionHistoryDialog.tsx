import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type FileEntry, type FileVersion } from '../api/client'

interface VersionHistoryDialogProps {
  file: FileEntry
  onClose: () => void
  onRestored: () => void
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

/**
 * Historial de versiones de un archivo (§15). Modal en vez de página propia:
 * está intrínsecamente ligado a un archivo concreto de la vista actual.
 */
export default function VersionHistoryDialog({ file, onClose, onRestored }: VersionHistoryDialogProps) {
  const [versions, setVersions] = useState<FileVersion[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [restoringVersion, setRestoringVersion] = useState<number | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setVersions(await api.listVersions(file.id))
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo cargar el historial.')
    } finally {
      setLoading(false)
    }
  }, [file.id])

  useEffect(() => {
    void load()
  }, [load])

  async function handleRestore(versionNum: number) {
    setRestoringVersion(versionNum)
    try {
      await api.restoreVersion(file.id, versionNum)
      onRestored()
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo restaurar esta versión.')
    } finally {
      setRestoringVersion(null)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="max-h-[80vh] w-full max-w-lg overflow-auto rounded-lg bg-white p-5 shadow-lg dark:bg-slate-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-start justify-between">
          <div>
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-50">Historial de versiones</h2>
            <p className="text-sm text-slate-500 dark:text-slate-400">{file.name}</p>
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

        <div className="mb-2 rounded-md border border-slate-200 px-3 py-2 text-sm dark:border-slate-800">
          <p className="font-medium text-slate-800 dark:text-slate-200">Versión actual</p>
          <p className="text-slate-500 dark:text-slate-400">
            {formatBytes(file.size_bytes)} · {formatDate(file.updated_at)}
          </p>
        </div>

        {loading ? (
          <p className="py-4 text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : versions.length === 0 ? (
          <p className="py-4 text-sm text-slate-500 dark:text-slate-400">Todavía no hay versiones anteriores de este archivo.</p>
        ) : (
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {versions.map((v) => (
              <li key={v.version_num} className="flex items-center justify-between py-2.5 text-sm">
                <div>
                  <p className="text-slate-800 dark:text-slate-200">Versión {v.version_num}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {formatBytes(v.size_bytes)} · {formatDate(v.created_at)}
                  </p>
                </div>
                <div>
                  <a
                    href={api.downloadVersionUrl(file.id, v.version_num)}
                    className="mr-3 rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                  >
                    Descargar
                  </a>
                  <button
                    disabled={restoringVersion !== null}
                    onClick={() => void handleRestore(v.version_num)}
                    className="rounded px-2 py-1 text-xs text-slate-700 hover:bg-slate-100 disabled:opacity-50 dark:text-slate-300 dark:hover:bg-slate-800"
                  >
                    {restoringVersion === v.version_num ? 'Restaurando…' : 'Restaurar'}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
