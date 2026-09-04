// Cliente de la API de NexusCloud (/api/v1, ver ../../../docs/api.md).
// Usa cookies de sesión (credentials: 'include') en vez de guardar el
// token en localStorage: el backend ya emite una cookie HttpOnly en el
// login, así que el JS de esta SPA nunca necesita leer ni almacenar el
// token él mismo (menor superficie ante XSS).

export interface User {
  id: string
  username: string
  display_name: string
  email?: string
  status: 'active' | 'disabled'
  has_totp: boolean
  created_at: string
  last_login_at?: string
}

export interface Session {
  id: string
  device?: string
  ip?: string
  created_at: string
  last_seen_at: string
  expires_at: string
}

export interface DirectoryEntry {
  id: string
  parent_path: string
  name: string
  created_at: string
  deleted_at?: string
}

export interface FileEntry {
  id: string
  parent_path: string
  name: string
  size_bytes: number
  sha256: string
  mime_type: string
  created_at: string
  updated_at: string
  deleted_at?: string
}

export interface ListResult {
  directories: DirectoryEntry[]
  files: FileEntry[]
}

export interface FileVersion {
  version_num: number
  size_bytes: number
  sha256: string
  mime_type: string
  created_at: string
}

export interface Group {
  id: string
  name: string
}

// Share refleja shareResponse (internal/api/v1/dto.go): token solo viene
// relleno en la respuesta de creación de un enlace, una única vez (§78).
export interface Share {
  id: string
  resource_type: 'file' | 'directory'
  resource_id: string
  resource_name?: string
  share_type: 'user' | 'group' | 'link'
  target_user_id?: string
  target_username?: string
  target_group_id?: string
  target_group_name?: string
  label?: string
  can_download: boolean
  can_upload: boolean
  has_password: boolean
  expires_at?: string
  max_downloads?: number
  download_count: number
  max_upload_size_bytes?: number
  created_at: string
  token?: string
}

export interface CreateShareInput {
  resource_type: 'file' | 'directory'
  resource_id: string
  share_type: 'user' | 'group' | 'link'
  target_username?: string
  target_group_id?: string
  label?: string
  can_download?: boolean
  can_upload?: boolean
  password?: string
  expires_at?: string
  max_downloads?: number
  max_upload_size_bytes?: number
}

// PublicShareInfo refleja publicShareInfoResponse: si el enlace tiene
// contraseña y no se envió una correcta, name/size_bytes/etc. vienen vacíos
// -- ver internal/api/v1/public_share_handlers.go.
export interface PublicShareInfo {
  requires_password: boolean
  password_incorrect?: boolean
  revoked?: boolean
  expired?: boolean
  exhausted?: boolean
  resource_type?: 'file' | 'directory'
  name?: string
  size_bytes?: number
  can_download?: boolean
  can_upload?: boolean
  label?: string
}

export class ApiClientError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiClientError'
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const isBodyPlainObject = init?.body !== undefined && typeof init.body === 'string'
  const res = await fetch(path, {
    ...init,
    credentials: 'include',
    headers: {
      ...(isBodyPlainObject ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })

  if (res.status === 204) {
    return undefined as T
  }

  const contentType = res.headers.get('content-type') ?? ''
  const isJson = contentType.includes('application/json')

  if (!res.ok) {
    if (isJson) {
      const body = (await res.json()) as { error?: { code: string; message: string } }
      throw new ApiClientError(res.status, body.error?.code ?? 'unknown', body.error?.message ?? 'Error desconocido')
    }
    throw new ApiClientError(res.status, 'unknown', `Error ${res.status}`)
  }

  if (isJson) {
    return (await res.json()) as T
  }
  return undefined as T
}

/** Sube contenido con progreso real (fetch no lo expone en subida). */
function uploadWithProgress(
  parentPath: string,
  name: string,
  content: Blob,
  onProgress?: (pct: number) => void,
): Promise<FileEntry> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const url = `/api/v1/files?name=${encodeURIComponent(name)}&path=${encodeURIComponent(parentPath)}`
    xhr.open('POST', url)
    xhr.withCredentials = true
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(JSON.parse(xhr.responseText) as FileEntry)
        return
      }
      try {
        const body = JSON.parse(xhr.responseText) as { error?: { code: string; message: string } }
        reject(new ApiClientError(xhr.status, body.error?.code ?? 'unknown', body.error?.message ?? 'Error al subir el archivo'))
      } catch {
        reject(new ApiClientError(xhr.status, 'unknown', `Error ${xhr.status}`))
      }
    }
    xhr.onerror = () => reject(new ApiClientError(0, 'network_error', 'Error de red durante la subida'))
    xhr.send(content)
  })
}

export const api = {
  login: (username: string, password: string, totp_code?: string) =>
    request<{ token: string; user: User; session: Session }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp_code }),
    }),
  logout: () => request<void>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => request<User>('/api/v1/users/me'),

  sessions: () => request<Session[]>('/api/v1/auth/sessions'),
  revokeSession: (id: string) => request<void>(`/api/v1/auth/sessions/${id}`, { method: 'DELETE' }),

  list: (path: string) => request<ListResult>(`/api/v1/files?path=${encodeURIComponent(path)}`),
  upload: uploadWithProgress,
  downloadUrl: (id: string) => `/api/v1/files/${id}`,
  // Por defecto mueve a la papelera (§16); permanent=true la salta.
  deleteFile: (id: string) => request<void>(`/api/v1/files/${id}`, { method: 'DELETE' }),
  deleteFileForever: (id: string) => request<void>(`/api/v1/files/${id}?permanent=true`, { method: 'DELETE' }),
  restoreFile: (id: string) => request<void>(`/api/v1/files/${id}/restore`, { method: 'POST' }),

  mkdir: (parent_path: string, name: string) =>
    request<DirectoryEntry>('/api/v1/directories', { method: 'POST', body: JSON.stringify({ parent_path, name }) }),
  deleteDirectory: (id: string) => request<void>(`/api/v1/directories/${id}`, { method: 'DELETE' }),
  deleteDirectoryForever: (id: string) => request<void>(`/api/v1/directories/${id}?permanent=true`, { method: 'DELETE' }),
  restoreDirectory: (id: string) => request<void>(`/api/v1/directories/${id}/restore`, { method: 'POST' }),

  trash: () => request<ListResult>('/api/v1/trash'),

  listVersions: (fileId: string) => request<FileVersion[]>(`/api/v1/files/${fileId}/versions`),
  downloadVersionUrl: (fileId: string, versionNum: number) => `/api/v1/files/${fileId}/versions/${versionNum}`,
  restoreVersion: (fileId: string, versionNum: number) =>
    request<FileEntry>(`/api/v1/files/${fileId}/versions/${versionNum}/restore`, { method: 'POST' }),

  listGroups: () => request<Group[]>('/api/v1/groups'),

  createShare: (input: CreateShareInput) => request<Share>('/api/v1/shares', { method: 'POST', body: JSON.stringify(input) }),
  listShares: (direction: 'by-me' | 'with-me') => request<Share[]>(`/api/v1/shares?direction=${direction}`),
  revokeShare: (id: string) => request<void>(`/api/v1/shares/${id}`, { method: 'DELETE' }),
  listSharedDirectory: (id: string) => request<ListResult>(`/api/v1/shared-directories/${id}`),

  // Enlaces públicos (§37): sin sesión, autorizados por el token de la URL
  // y una contraseña opcional que va SIEMPRE en la cabecera X-Share-Password
  // -- nunca en la query string, para no dejarla en logs/historial/Referer.
  publicShareInfo: (token: string, password?: string) =>
    request<PublicShareInfo>(`/api/v1/public/shares/${token}`, {
      headers: password ? { 'X-Share-Password': password } : undefined,
    }),
  publicShareBrowse: (token: string, path: string, password?: string) =>
    request<ListResult>(`/api/v1/public/shares/${token}/browse?path=${encodeURIComponent(path)}`, {
      headers: password ? { 'X-Share-Password': password } : undefined,
    }),
  /**
   * Descarga vía fetch + Blob en vez de un <a href> plano: un enlace con
   * contraseña no puede llevar cabeceras personalizadas, así que la SPA hace
   * la petición ella misma y dispara el guardado en el navegador.
   */
  downloadPublicShare: async (token: string, path: string, filename: string, password?: string): Promise<void> => {
    const url = `/api/v1/public/shares/${token}/download${path ? `?path=${encodeURIComponent(path)}` : ''}`
    const res = await fetch(url, { headers: password ? { 'X-Share-Password': password } : undefined })
    if (!res.ok) {
      const body = (await res.json().catch(() => null)) as { error?: { code: string; message: string } } | null
      throw new ApiClientError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? 'Error al descargar')
    }
    const blob = await res.blob()
    const blobUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = blobUrl
    a.download = filename
    a.click()
    URL.revokeObjectURL(blobUrl)
  },
  uploadToPublicShare: async (token: string, path: string, name: string, content: Blob, password?: string): Promise<FileEntry> => {
    const url = `/api/v1/public/shares/${token}/upload?path=${encodeURIComponent(path)}&name=${encodeURIComponent(name)}`
    const res = await fetch(url, { method: 'POST', headers: password ? { 'X-Share-Password': password } : undefined, body: content })
    if (!res.ok) {
      const body = (await res.json().catch(() => null)) as { error?: { code: string; message: string } } | null
      throw new ApiClientError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? 'Error al subir el archivo')
    }
    return (await res.json()) as FileEntry
  },
}
