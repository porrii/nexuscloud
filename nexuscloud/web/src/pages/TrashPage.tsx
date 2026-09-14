import { useCallback, useEffect, useState } from 'react'
import { api, ApiClientError, type DirectoryEntry, type FileEntry, type ListResult } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog'

type PendingForever = { kind: 'file'; entry: FileEntry } | { kind: 'directory'; entry: DirectoryEntry }

function formatDate(iso?: string): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('es-ES', { dateStyle: 'medium', timeStyle: 'short' })
}

export default function TrashPage() {
  const [result, setResult] = useState<ListResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pendingForever, setPendingForever] = useState<PendingForever | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setResult(await api.trash())
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo cargar la papelera.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function handleRestoreFile(id: string) {
    try {
      await api.restoreFile(id)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo restaurar el archivo.')
    }
  }

  async function handleRestoreDirectory(id: string) {
    try {
      await api.restoreDirectory(id)
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo restaurar la carpeta.')
    }
  }

  async function handleConfirmForever() {
    if (!pendingForever) return
    try {
      if (pendingForever.kind === 'file') {
        await api.deleteFileForever(pendingForever.entry.id)
      } else {
        await api.deleteDirectoryForever(pendingForever.entry.id)
      }
      setPendingForever(null)
      await load()
    } catch (err) {
      setPendingForever(null)
      setError(err instanceof ApiClientError ? err.message : 'No se pudo eliminar definitivamente.')
    }
  }

  const isEmpty = (result?.directories.length ?? 0) === 0 && (result?.files.length ?? 0) === 0

  return (
    <div className="flex h-full flex-col p-6">
      <div className="mb-4">
        <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-50">Papelera</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Los elementos eliminados se purgan automáticamente tras un tiempo de retención configurado por el administrador.
        </p>
      </div>

      {error && (
        <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {error}
        </div>
      )}

      <div className="flex-1 overflow-auto rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        {loading ? (
          <p className="p-6 text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : isEmpty ? (
          <div className="flex h-full flex-col items-center justify-center gap-1 p-12 text-center">
            <p className="text-sm font-medium text-slate-600 dark:text-slate-300">La papelera está vacía</p>
          </div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800 dark:text-slate-400">
              <tr>
                <th className="px-4 py-2 font-medium">Nombre</th>
                <th className="px-4 py-2 font-medium">Ubicación original</th>
                <th className="px-4 py-2 font-medium">Eliminado</th>
                <th className="px-4 py-2 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {result?.directories.map((d) => (
                <tr key={d.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                  <td className="px-4 py-2">
                    <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                      <span aria-hidden>📁</span>
                      {d.name}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{d.parent_path}</td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatDate(d.deleted_at)}</td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => void handleRestoreDirectory(d.id)}
                      className="mr-3 rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                    >
                      Restaurar
                    </button>
                    <button
                      onClick={() => setPendingForever({ kind: 'directory', entry: d })}
                      className="rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
                    >
                      Eliminar para siempre
                    </button>
                  </td>
                </tr>
              ))}
              {result?.files.map((f) => (
                <tr key={f.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                  <td className="px-4 py-2">
                    <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                      <span aria-hidden>📄</span>
                      {f.name}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{f.parent_path}</td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatDate(f.deleted_at)}</td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => void handleRestoreFile(f.id)}
                      className="mr-3 rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                    >
                      Restaurar
                    </button>
                    <button
                      onClick={() => setPendingForever({ kind: 'file', entry: f })}
                      className="rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
                    >
                      Eliminar para siempre
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {pendingForever && (
        <ConfirmDialog
          title="Eliminar para siempre"
          message={`"${pendingForever.entry.name}" se eliminará definitivamente y no podrá recuperarse. ¿Continuar?`}
          confirmLabel="Eliminar para siempre"
          danger
          onConfirm={() => void handleConfirmForever()}
          onCancel={() => setPendingForever(null)}
        />
      )}
    </div>
  )
}
