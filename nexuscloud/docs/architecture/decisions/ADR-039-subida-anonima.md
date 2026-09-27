# ADR-039: Subida anónima

## Estado

Aceptado.

## Contexto

§38 pide: *"Permitir enlaces de subida sin acceso al resto del contenido
cuando el administrador lo autorice. Desactivado por defecto. Protegido
contra abuso. Limitado. Auditado."* Es el último hueco de Sharing (§37,
ADR-035) que quedaba sin implementar, y varios sitios del repo
(`docs/storage.md`, ADR-008, ADR-035, `docs/security/threat-model.md`, un
comentario en `file_service_sharing.go`) ya lo señalaban como "modelo
separado", sin que existiera código que lo demostrara.

La exploración confirmó por qué hace falta un modelo separado y no una
variante de `Share` con una bandera: el modelo `Share` actual **no
garantiza de verdad** "solo subida, sin ver el resto". `BrowsePublicShare`
ignora por completo `CanDownload` -- un enlace público con
`CanUpload=true, CanDownload=false` sigue dejando listar recursivamente
toda la carpeta hoy. Reutilizar `shares` habría heredado ese defecto; §38
necesita su propia ruta pública, sin ningún endpoint de listado, para que
la garantía sea estructural (no existe la posibilidad de navegar, no una
comprobación que se pueda olvidar) en vez de un permiso que hay que
recordar respetar en cada sitio.

## Decisión

1. **Modelo nuevo (`anonymous_uploads`), no una variante de `Share`.**
   Reutiliza el mismo generador/hash de token que ya usan sesiones y shares
   (`idgen.Token()` + `hashShareToken`, tal cual, sin duplicarlo), pero
   tabla, struct y endpoints propios. Migración `0015_anonymous_uploads`:
   `id, owner_id, directory_id NOT NULL, token_hash NOT NULL UNIQUE, label,
   max_upload_size_bytes, expires_at, revoked_at, upload_count, created_at`.
   `directory_id` es `NOT NULL` -- a diferencia de `favorites` (ADR-038),
   que sí es polimórfico file/directory -- porque un enlace de subida
   anónima SIEMPRE apunta a una carpeta, nunca a un archivo suelto.
   `token_hash` es `NOT NULL UNIQUE` directo, sin índice parcial -- a
   diferencia de `shares.token_hash` (nullable, solo para el tipo `link`),
   aquí SIEMPRE hay token. `upload_count` es informativo para el
   propietario, mismo papel que `Share.DownloadCount`, sin aplicar ningún
   límite sobre él en esta primera versión.

2. **Cualquier usuario puede crear sus propios enlaces, una vez el
   administrador activa la función globalmente** (`sharing.
   anonymousUploadEnabled`, `false` por defecto -- mismo patrón que
   `webdav.enabled`/`sharing.publicLinksEnabled`), no aprobación caso por
   caso. El creador del enlace elige una carpeta SUYA como destino, igual
   mecanismo que un enlace normal de Sharing hoy -- no un "buzón" especial a
   nivel de instancia designado por el administrador.

3. **Sin contraseña.** A diferencia de `Share`, §38 no la pide, y añadir una
   segunda capa de secreto sin necesidad clara todavía es superficie sin
   beneficio demostrado.

4. **Anti-abuso de esta primera versión: límite de tamaño por archivo**
   (reutiliza `MaxUploadSizeBytes`/`errLimitReader`, ya probado por
   `UploadViaPublicShare`/`UploadToSharedDirectory`) **+ un límite de
   peticiones por minuto propio y más estricto que el de enlaces normales**
   (`security.rateLimit.anonymousUploadPerMinute`, por defecto **10** frente
   a los 20 de `publicLinkPerMinute` -- aquí cualquiera con el enlace
   escribe sin que el creador haya podido vetar a nadie de antemano) **+
   expiración opcional** (igual que hoy en `shares`). Sin límite acumulado
   de tamaño total ni expiración obligatoria: se añaden después si hace
   falta de verdad, no especulativamente.

5. **Ningún endpoint de navegación existe para este modelo, a propósito.**
   `file_service_anonymous_upload.go` no tiene ningún método de listado; el
   único gesto público posible es `POST
   /public/anonymous-uploads/{token}/upload`, que sube directo a la raíz de
   la carpeta del enlace (mismo mecanismo de resolución de ruta que
   `UploadToSharedDirectory`, no la variante con `SubPath` de
   `UploadViaPublicShare`, que aquí no hace falta porque no hay sub-ruta que
   resolver). El probe público (`GET /public/anonymous-uploads/{token}`)
   solo revela `label` y `max_upload_size_bytes` si el enlace es válido.

6. **Un único 404 genérico para inexistente/revocado/caducado**, tanto en el
   probe como en la subida -- a diferencia de `GetPublicShare`/
   `writePublicShareError` (que sí distinguen revocado de caducado con
   mensajes propios, útil ahí porque el creador del share puede estar
   mirando desde el otro lado). En §38 quien solo tiene el token no necesita
   distinguir el motivo, y no distinguirlo reduce la superficie de
   enumeración para quien esté probando tokens al azar.

7. **Cuota y titularidad: el archivo es del propietario de la carpeta**,
   igual criterio que `UploadToSharedDirectory`/`UploadViaPublicShare`
   (ADR-035, ADR-036) -- la cuota que se comprueba y se descuenta es la
   suya, nunca la de quien sube (que ni siquiera tiene cuenta). Un 403/507
   de cuota agotada usa el mismo mensaje genérico ya establecido
   (`writeNoSpaceInFolder`): un anónimo no debe averiguar cuánto usa ni
   cuánto tiene el propietario. `NoOverwrite: true` siempre (ADR-035 punto
   4): nunca pisa un archivo existente ni revela la papelera del
   propietario.

8. **Auditoría:** `anonymous_upload_link_created`/`_revoked` son eventos
   propios; la subida en sí reutiliza `upload` con `via: "anonymous_upload"`
   en los metadatos -- mismo criterio que `via: "shared_directory"`/
   `"public_share"` ya existentes.

9. **Sin CLI**, mismo motivo que Sharing normal (`shares`): autoservicio
   web/API para un usuario ya conectado, sin el caso de uso "provisionar sin
   navegador" que sí justificó la CLI de los tokens WebDAV/API (ADR-034,
   ADR-037).

10. **Web:** botón nuevo solo en filas de CARPETA (`FilesPage.tsx`,
    "Subida anónima") que abre `AnonymousUploadDialog.tsx` -- mismo espíritu
    que `ShareDialog.tsx` pero para este modelo separado: crear con
    label/límite/expiración opcionales, panel "cópialo ahora" con el token,
    lista de enlaces activos con revocar. Página pública nueva en
    `/u/:token` (`AnonymousUploadPage.tsx`, fuera del shell autenticado,
    mismo nivel que `/s/:token`), sin navegación ni contraseña: solo una
    zona de subida.

## Alternativas consideradas

- **Variante de `Share` con `CanUpload=true, CanDownload=false`.**
  Descartada: `BrowsePublicShare` ignora `CanDownload` hoy, así que no
  garantiza de verdad "sin acceso al resto" -- el propio defecto que §38
  pide evitar.
- **Buzón fijo a nivel de instancia** (una carpeta especial designada por el
  administrador para todos los enlaces anónimos). Descartada: más simple
  para el administrador pero menos flexible que dejar a cada usuario elegir
  su propia carpeta, y sin ningún pedido explícito de §38 en ese sentido.
- **Aprobación por enlace.** Descartada por exceso de fricción frente al
  patrón ya establecido por `webdav.enabled`/`publicLinksEnabled`:
  activación global del administrador, autoservicio después.

## Consecuencias

- Nueva tabla `anonymous_uploads`, sin tocar ninguna existente. Las
  instalaciones actuales no cambian de comportamiento hasta que el
  administrador activa `sharing.anonymousUploadEnabled`.
- Nuevo cubo de rate limit (`anonymousUploadLimiter`) independiente de
  `publicLimiter`: un enlace de subida anónima bajo abuso no afecta a los
  enlaces de compartición normales ni a la API autenticada.
- **Verificación:** 12 tests en `internal/storage` (crear sobre carpeta
  propia, rechazo sobre carpeta ajena, desactivado sin `WithAnonymousUploads`
  y con `enabled=false`, subida con éxito y contador, revocado/caducado,
  token inexistente, límite de tamaño sin dejar archivo parcial, nunca
  sobrescribe, carpeta trasheada, desactivado global corta un enlace ya
  existente, IDOR al revocar, aislamiento por propietario al listar), 5 de
  integración HTTP (flujo completo con auditoría, 401 en las rutas privadas
  sin sesión, 403 sobre carpeta ajena, desactivado por defecto, límite de
  peticiones propio e independiente de la API), 3 en `internal/config`
  (`anonymousUploadEnabled` false por defecto, `anonymousUploadPerMinute`
  con valor por defecto útil, rechazo de 0). Migración 0015 verificada
  aplicando el esquema completo en MySQL 8 y PostgreSQL 16 reales, ida y
  comprobación de idempotencia. Build/lint/tsc de la web.
