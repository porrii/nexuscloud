import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, ApiClientError, type DirectoryEntry, type FileEntry, type ListResult } from '../api/client'
import Breadcrumbs from '../components/Breadcrumbs'
import ConfirmDialog from '../components/ConfirmDialog'
import ShareDialog from '../components/ShareDialog'
import VersionHistoryDialog from '../components/VersionHistoryDialog'
import { notifyUsageChanged } from '../quota'

interface UploadProgress {
  key: string
  name: string
  percent: number
  error?: string
}

type PendingDelete = { kind: 'file'; entry: FileEntry } | { kind: 'directory'; entry: DirectoryEntry }

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

export default function FilesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const path = searchParams.get('path') || '/'

  const [result, setResult] = useState<ListResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [uploads, setUploads] = useState<UploadProgress[]>([])
  const [dragOver, setDragOver] = useState(false)
  const [newFolderOpen, setNewFolderOpen] = useState(false)
  const [newFolderName, setNewFolderName] = useState('')
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null)
  const [historyFile, setHistoryFile] = useState<FileEntry | null>(null)
  const [shareTarget, setShareTarget] = useState<{ id: string; name: string; isDirectory: boolean } | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const navigateTo = useCallback(
    (newPath: string) => {
      setSearchParams(newPath === '/' ? {} : { path: newPath })
    },
    [setSearchParams],
  )

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setResult(await api.list(path))
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo cargar el contenido de esta carpeta.')
    } finally {
      setLoading(false)
    }
  }, [path])

  useEffect(() => {
    void load()
  }, [load])

  async function handleUploadFiles(files: FileList | File[]) {
    for (const file of Array.from(files)) {
      const key = `${file.name}-${Date.now()}`
      setUploads((prev) => [...prev, { key, name: file.name, percent: 0 }])
      try {
        await api.upload(path, file.name, file, (percent) => {
          setUploads((prev) => prev.map((u) => (u.key === key ? { ...u, percent } : u)))
        })
        setUploads((prev) => prev.filter((u) => u.key !== key))
        notifyUsageChanged()
        await load()
      } catch (err) {
        const message = err instanceof ApiClientError ? err.message : 'Error al subir el archivo.'
        setUploads((prev) => prev.map((u) => (u.key === key ? { ...u, error: message } : u)))
      }
    }
  }

  async function handleCreateFolder() {
    const name = newFolderName.trim()
    if (!name) return
    try {
      await api.mkdir(path, name)
      setNewFolderOpen(false)
      setNewFolderName('')
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo crear la carpeta.')
    }
  }

  // toggleFavorite (§87, ADR-038): recarga el listado tras cambiar, igual
  // criterio que el resto de acciones de esta página (crear carpeta,
  // borrar...) -- así favorite_id siempre sale de la verdad del servidor,
  // nunca de una actualización optimista local.
  async function toggleFavorite(entry: { id: string; favorite_id?: string }, resourceType: 'file' | 'directory') {
    try {
      if (entry.favorite_id) {
        await api.removeFavorite(entry.favorite_id)
      } else {
        await api.addFavorite(resourceType, entry.id)
      }
      await load()
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : 'No se pudo actualizar el favorito.')
    }
  }

  async function handleConfirmDelete() {
    if (!pendingDelete) return
    try {
      if (pendingDelete.kind === 'file') {
        await api.deleteFile(pendingDelete.entry.id)
      } else {
        await api.deleteDirectory(pendingDelete.entry.id)
      }
      setPendingDelete(null)
      notifyUsageChanged() // con papelera el espacio no se libera, pero el desglose cambia
      await load()
    } catch (err) {
      setPendingDelete(null)
      setError(
        err instanceof ApiClientError && err.code === 'not_empty'
          ? 'Esa carpeta no está vacía: elimina primero su contenido.'
          : err instanceof ApiClientError
            ? err.message
            : 'No se pudo eliminar.',
      )
    }
  }

  return (
    <div
      className="flex h-full flex-col p-6"
      onDragOver={(e) => {
        e.preventDefault()
        setDragOver(true)
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDragOver(false)
        if (e.dataTransfer.files.length) void handleUploadFiles(e.dataTransfer.files)
      }}
    >
      <div className="mb-4 flex items-center justify-between">
        <Breadcrumbs path={path} onNavigate={navigateTo} />
        <div className="flex gap-2">
          <button
            onClick={() => setNewFolderOpen(true)}
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            Nueva carpeta
          </button>
          <button
            onClick={() => fileInputRef.current?.click()}
            className="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700"
          >
            Subir archivo
          </button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files?.length) void handleUploadFiles(e.target.files)
              e.target.value = ''
            }}
          />
        </div>
      </div>

      {newFolderOpen && (
        <div className="mb-4 flex items-center gap-2 rounded-md border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900">
          <input
            autoFocus
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void handleCreateFolder()
              if (e.key === 'Escape') setNewFolderOpen(false)
            }}
            placeholder="Nombre de la carpeta"
            className="flex-1 rounded-md border border-slate-300 px-3 py-1.5 text-sm outline-none focus:border-blue-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
          />
          <button onClick={() => void handleCreateFolder()} className="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700">
            Crear
          </button>
          <button
            onClick={() => setNewFolderOpen(false)}
            className="rounded-md px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            Cancelar
          </button>
        </div>
      )}

      {uploads.length > 0 && (
        <div className="mb-4 space-y-1.5">
          {uploads.map((u) => (
            <div key={u.key} className="rounded-md border border-slate-200 bg-white px-3 py-2 text-sm dark:border-slate-800 dark:bg-slate-900">
              <div className="flex justify-between">
                <span className="truncate text-slate-700 dark:text-slate-300">{u.name}</span>
                <span className={u.error ? 'text-red-600 dark:text-red-400' : 'text-slate-500'}>{u.error ?? `${u.percent}%`}</span>
              </div>
              {!u.error && (
                <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
                  <div className="h-full bg-blue-600 transition-all" style={{ width: `${u.percent}%` }} />
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {error && (
        <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {error}
        </div>
      )}

      <div
        className={`flex-1 overflow-auto rounded-lg border ${
          dragOver ? 'border-blue-400 bg-blue-50/50 dark:bg-blue-950/20' : 'border-slate-200 dark:border-slate-800'
        } bg-white dark:bg-slate-900`}
      >
        {loading ? (
          <p className="p-6 text-sm text-slate-500 dark:text-slate-400">Cargando…</p>
        ) : (result?.directories.length ?? 0) === 0 && (result?.files.length ?? 0) === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-1 p-12 text-center">
            <p className="text-sm font-medium text-slate-600 dark:text-slate-300">Esta carpeta está vacía</p>
            <p className="text-sm text-slate-400 dark:text-slate-500">Arrastra archivos aquí o usa "Subir archivo"</p>
          </div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b border-slate-200 text-xs uppercase text-slate-500 dark:border-slate-800 dark:text-slate-400">
              <tr>
                <th className="px-4 py-2 font-medium">Nombre</th>
                <th className="px-4 py-2 font-medium">Tamaño</th>
                <th className="px-4 py-2 font-medium">Modificado</th>
                <th className="px-4 py-2 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {result?.directories.map((d) => (
                <tr key={d.id} className="group hover:bg-slate-50 dark:hover:bg-slate-800/50">
                  <td className="px-4 py-2">
                    <button
                      onClick={() => navigateTo(`${path === '/' ? '' : path}/${d.name}`)}
                      className="flex items-center gap-2 font-medium text-slate-800 hover:text-blue-700 dark:text-slate-200 dark:hover:text-blue-400"
                    >
                      <span aria-hidden>📁</span>
                      {d.name}
                    </button>
                  </td>
                  <td className="px-4 py-2 text-slate-400">—</td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatDate(d.created_at)}</td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => void toggleFavorite(d, 'directory')}
                      title={d.favorite_id ? 'Quitar de favoritos' : 'Marcar como favorito'}
                      className={`mr-3 rounded px-2 py-1 text-xs ${
                        d.favorite_id
                          ? 'text-amber-500 hover:bg-amber-50 dark:hover:bg-amber-950'
                          : 'invisible text-slate-400 hover:bg-slate-100 group-hover:visible dark:hover:bg-slate-800'
                      }`}
                    >
                      {d.favorite_id ? '★' : '☆'}
                    </button>
                    <button
                      onClick={() => setShareTarget({ id: d.id, name: d.name, isDirectory: true })}
                      className="invisible mr-3 rounded px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 group-hover:visible dark:text-slate-300 dark:hover:bg-slate-800"
                    >
                      Compartir
                    </button>
                    <button
                      onClick={() => setPendingDelete({ kind: 'directory', entry: d })}
                      className="invisible rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 group-hover:visible dark:text-red-400 dark:hover:bg-red-950"
                    >
                      Eliminar
                    </button>
                  </td>
                </tr>
              ))}
              {result?.files.map((f) => (
                <tr key={f.id} className="group hover:bg-slate-50 dark:hover:bg-slate-800/50">
                  <td className="px-4 py-2">
                    <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                      <span aria-hidden>📄</span>
                      {f.name}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatBytes(f.size_bytes)}</td>
                  <td className="px-4 py-2 text-slate-500 dark:text-slate-400">{formatDate(f.updated_at)}</td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => void toggleFavorite(f, 'file')}
                      title={f.favorite_id ? 'Quitar de favoritos' : 'Marcar como favorito'}
                      className={`mr-3 rounded px-2 py-1 text-xs ${
                        f.favorite_id
                          ? 'text-amber-500 hover:bg-amber-50 dark:hover:bg-amber-950'
                          : 'invisible text-slate-400 hover:bg-slate-100 group-hover:visible dark:hover:bg-slate-800'
                      }`}
                    >
                      {f.favorite_id ? '★' : '☆'}
                    </button>
                    <a
                      href={api.downloadUrl(f.id)}
                      download={f.name}
                      className="mr-3 rounded px-2 py-1 text-xs text-blue-700 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                    >
                      Descargar
                    </a>
                    <button
                      onClick={() => setShareTarget({ id: f.id, name: f.name, isDirectory: false })}
                      className="invisible mr-3 rounded px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 group-hover:visible dark:text-slate-300 dark:hover:bg-slate-800"
                    >
                      Compartir
                    </button>
                    <button
                      onClick={() => setHistoryFile(f)}
                      className="invisible mr-3 rounded px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 group-hover:visible dark:text-slate-300 dark:hover:bg-slate-800"
                    >
                      Historial
                    </button>
                    <button
                      onClick={() => setPendingDelete({ kind: 'file', entry: f })}
                      className="invisible rounded px-2 py-1 text-xs text-red-600 hover:bg-red-50 group-hover:visible dark:text-red-400 dark:hover:bg-red-950"
                    >
                      Eliminar
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {pendingDelete && (
        <ConfirmDialog
          title={pendingDelete.kind === 'file' ? 'Mover a la papelera' : 'Mover carpeta a la papelera'}
          message={`"${pendingDelete.entry.name}" se moverá a la papelera. Podrás restaurarlo desde ahí mientras no se purgue automáticamente.`}
          confirmLabel="Mover a la papelera"
          danger
          onConfirm={() => void handleConfirmDelete()}
          onCancel={() => setPendingDelete(null)}
        />
      )}

      {historyFile && (
        <VersionHistoryDialog file={historyFile} onClose={() => setHistoryFile(null)} onRestored={() => void load()} />
      )}

      {shareTarget && <ShareDialog resource={shareTarget} onClose={() => setShareTarget(null)} />}
    </div>
  )
}
