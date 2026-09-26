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

export type QuotaSource = 'user' | 'group' | 'global' | 'none'

// Quota refleja GET /users/me/quota (§24, ADR-036): lo que ocupa quien pregunta
// (archivos + papelera + versiones anteriores) y su límite efectivo. limit_bytes
// se omite si no tiene límite; source dice de dónde sale.
export interface Quota {
  used_bytes: number
  files_bytes: number
  trash_bytes: number
  versions_bytes: number
  limit_bytes?: number
  source: QuotaSource
  group_name?: string
}

export interface Session {
  id: string
  device?: string
  ip?: string
  created_at: string
  last_seen_at: string
  expires_at: string
}

// WebAuthnCredential refleja webauthnCredentialResponse (internal/api/v1/
// webauthn_handlers.go): nunca expone credential_id/public_key/sign_count
// (§172), igual criterio que Session con TokenHash.
export interface WebAuthnCredential {
  id: string
  label: string
  created_at: string
  last_used_at?: string
}

// WebDAVToken refleja webdavTokenResponse (internal/api/v1/webdav_handlers.go):
// nunca expone el hash (§172). CreatedWebDAVToken añade el token en claro, que
// solo viene en la respuesta de creación, una única vez (§78), y la ruta donde
// está montado WebDAV en este servidor (configurable: webdav.path).
export interface WebDAVToken {
  id: string
  label: string
  created_at: string
  last_used_at?: string
}

export interface CreatedWebDAVToken extends WebDAVToken {
  token: string
  webdav_path: string
}

// ApiToken refleja apiTokenResponse (internal/api/v1/api_token_handlers.go):
// nunca expone el hash (§172). CreatedApiToken añade el token en claro, que
// solo viene en la respuesta de creación, una única vez (§78, ADR-037). A
// diferencia del token WebDAV, autentica contra la API REST completa (no
// solo WebDAV) y admite una expiración opcional (expires_at).
export interface ApiToken {
  id: string
  label: string
  created_at: string
  expires_at?: string
  last_used_at?: string
}

export interface CreatedApiToken extends ApiToken {
  token: string
}

export interface DirectoryEntry {
  id: string
  parent_path: string
  name: string
  created_at: string
  deleted_at?: string
  // favorite_id (§87, ADR-038): presente = favorito, y es el ID a pasar a
  // api.removeFavorite. Solo lo anota GET /files (árbol propio y activo).
  favorite_id?: string
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
  favorite_id?: string
}

export interface ListResult {
  directories: DirectoryEntry[]
  files: FileEntry[]
}

// Favorite refleja favoriteResponse (internal/api/v1/favorite_handlers.go).
export interface Favorite {
  id: string
  resource_type: 'file' | 'directory'
  resource_id: string
  created_at: string
}

// ActivityEvent refleja activityEventResponse (§88, ADR-038): el texto
// humano ("Fulano subió X, hace 5 minutos") se construye en el cliente a
// partir de event_type + metadata, ver describeActivity en FavoritesPage.
export interface ActivityEvent {
  id: string
  occurred_at: string
  event_type: string
  target_type?: string
  target_id?: string
  metadata?: Record<string, unknown>
}

// SharedDirectoryListing refleja sharedListingResponse (internal/api/v1/
// share_handlers.go): el listado de una carpeta compartida con quien la mira,
// más si esa persona puede subir a ella y con qué límite por archivo (§37,
// ADR-035). max_upload_size_bytes ausente = sin límite.
export interface SharedDirectoryListing extends ListResult {
  can_upload: boolean
  max_upload_size_bytes?: number
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

/** POST de un archivo con progreso real (fetch no lo expone en subida). */
function postWithProgress(url: string, content: Blob, onProgress?: (pct: number) => void): Promise<FileEntry> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
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

function uploadWithProgress(
  parentPath: string,
  name: string,
  content: Blob,
  onProgress?: (pct: number) => void,
): Promise<FileEntry> {
  return postWithProgress(
    `/api/v1/files?name=${encodeURIComponent(name)}&path=${encodeURIComponent(parentPath)}`,
    content,
    onProgress,
  )
}

/**
 * Sube a una carpeta que otra persona ha compartido contigo con permiso de
 * subida (§37, ADR-035). El destino sale solo del ID de la carpeta, nunca de
 * una ruta que el cliente pueda manipular; el archivo queda en el árbol del
 * propietario y no sobrescribe uno existente (409 destination_occupied).
 */
function uploadToSharedDirectory(
  directoryId: string,
  name: string,
  content: Blob,
  onProgress?: (pct: number) => void,
): Promise<FileEntry> {
  return postWithProgress(`/api/v1/shared-directories/${directoryId}/files?name=${encodeURIComponent(name)}`, content, onProgress)
}

export const api = {
  login: (username: string, password: string, totp_code?: string) =>
    request<{ token: string; user: User; session: Session }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp_code }),
    }),
  logout: () => request<void>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => request<User>('/api/v1/users/me'),
  quota: () => request<Quota>('/api/v1/users/me/quota'),

  sessions: () => request<Session[]>('/api/v1/auth/sessions'),
  revokeSession: (id: string) => request<void>(`/api/v1/auth/sessions/${id}`, { method: 'DELETE' }),

  // Passkeys / WebAuthn (§25, ADR-033). Usa los métodos JSON nativos del
  // propio estándar WebAuthn L3 (PublicKeyCredential.parseCreationOptionsFromJSON/
  // parseRequestOptionsFromJSON, credential.toJSON()) en vez de una librería
  // aparte: ya están disponibles en los navegadores modernos que hacen
  // falta para que WebAuthn funcione de todas formas, así que añadir una
  // dependencia solo para esto sería redundante.
  listWebAuthnCredentials: () => request<WebAuthnCredential[]>('/api/v1/auth/webauthn/credentials'),
  revokeWebAuthnCredential: (id: string) => request<void>(`/api/v1/auth/webauthn/credentials/${id}`, { method: 'DELETE' }),

  /** Ceremonia completa de alta: pide el reto, lo resuelve el propio navegador/authenticator, y lo confirma. */
  registerWebAuthnCredential: async (label: string): Promise<WebAuthnCredential> => {
    const begin = await request<{ ceremony_id: string; publicKey: PublicKeyCredentialCreationOptionsJSON }>(
      '/api/v1/auth/webauthn/register/begin',
      { method: 'POST' },
    )
    const options = PublicKeyCredential.parseCreationOptionsFromJSON(begin.publicKey)
    const credential = await navigator.credentials.create({ publicKey: options })
    if (!(credential instanceof PublicKeyCredential)) {
      throw new ApiClientError(0, 'webauthn_unsupported', 'El navegador no completó el registro del passkey.')
    }
    return request<WebAuthnCredential>(
      `/api/v1/auth/webauthn/register/finish?ceremony_id=${encodeURIComponent(begin.ceremony_id)}&label=${encodeURIComponent(label)}`,
      { method: 'POST', body: JSON.stringify(credential.toJSON()) },
    )
  },

  /**
   * Inicia un login con passkey: con username+password (ya verificados,
   * sin completar sesión todavía) es el segundo factor de esa cuenta; sin
   * ellos es login passwordless discoverable -- ver BeginWebAuthnLogin en
   * el backend.
   */
  beginWebAuthnLogin: (username?: string, password?: string) =>
    request<{ ceremony_id: string; publicKey: PublicKeyCredentialRequestOptionsJSON }>('/api/v1/auth/webauthn/login/begin', {
      method: 'POST',
      body: JSON.stringify(username ? { username, password } : {}),
    }),

  /** Resuelve el reto de beginWebAuthnLogin con el propio navegador/authenticator y completa el login. */
  finishWebAuthnLoginWithChallenge: async (
    ceremonyId: string,
    username: string | undefined,
    publicKey: PublicKeyCredentialRequestOptionsJSON,
  ): Promise<{ token: string; user: User; session: Session }> => {
    const options = PublicKeyCredential.parseRequestOptionsFromJSON(publicKey)
    const assertion = await navigator.credentials.get({ publicKey: options })
    if (!(assertion instanceof PublicKeyCredential)) {
      throw new ApiClientError(0, 'webauthn_unsupported', 'El navegador no completó el login con el passkey.')
    }
    const qs = new URLSearchParams({ ceremony_id: ceremonyId })
    if (username) qs.set('username', username)
    return request(`/api/v1/auth/webauthn/login/finish?${qs.toString()}`, {
      method: 'POST',
      body: JSON.stringify(assertion.toJSON()),
    })
  },

  // Tokens de acceso WebDAV (§43, ADR-034): la contraseña que usan los clientes
  // WebDAV por HTTP Basic (nunca la contraseña de la cuenta). Las rutas solo
  // existen si el servidor tiene webdav.enabled=true; con 404 la UI oculta la
  // sección, igual que Passkeys.
  listWebDAVTokens: () => request<WebDAVToken[]>('/api/v1/auth/webdav/tokens'),
  createWebDAVToken: (label: string) =>
    request<CreatedWebDAVToken>('/api/v1/auth/webdav/tokens', { method: 'POST', body: JSON.stringify({ label }) }),
  revokeWebDAVToken: (id: string) => request<void>(`/api/v1/auth/webdav/tokens/${id}`, { method: 'DELETE' }),

  // Tokens de API (§78, ADR-037): acceso todo-o-nada a la API REST completa
  // -- el token actúa exactamente como el usuario. A diferencia de WebDAV,
  // siempre están disponibles (sin opción de config que los desactive).
  apiTokens: () => request<ApiToken[]>('/api/v1/auth/api-tokens'),
  createApiToken: (label: string, expiresAt?: string) =>
    request<CreatedApiToken>('/api/v1/auth/api-tokens', {
      method: 'POST',
      body: JSON.stringify({ label, expires_at: expiresAt }),
    }),
  revokeApiToken: (id: string) => request<void>(`/api/v1/auth/api-tokens/${id}`, { method: 'DELETE' }),

  list: (path: string) => request<ListResult>(`/api/v1/files?path=${encodeURIComponent(path)}`),

  // Favoritos (§87, ADR-038): solo sobre el árbol propio del usuario.
  favorites: () => request<ListResult>('/api/v1/favorites'),
  addFavorite: (resourceType: 'file' | 'directory', resourceId: string) =>
    request<Favorite>('/api/v1/favorites', {
      method: 'POST',
      body: JSON.stringify({ resource_type: resourceType, resource_id: resourceId }),
    }),
  removeFavorite: (id: string) => request<void>(`/api/v1/favorites/${id}`, { method: 'DELETE' }),

  // Actividad reciente (§88, ADR-038): feed de "Recientes" del dashboard.
  activity: (limit?: number) => request<ActivityEvent[]>(`/api/v1/activity${limit ? `?limit=${limit}` : ''}`),
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
  listSharedDirectory: (id: string) => request<SharedDirectoryListing>(`/api/v1/shared-directories/${id}`),
  uploadToSharedDirectory,

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
