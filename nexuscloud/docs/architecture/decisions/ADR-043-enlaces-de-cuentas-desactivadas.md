# ADR-043: Enlaces públicos y de subida anónima de cuentas desactivadas

## Estado

Aceptado (2026-10-09). Complementa ADR-008
(sharing), ADR-034 (WebDAV), ADR-037 (tokens de API) y ADR-039 (subida
anónima). Ticket del backlog: «Cortar enlaces públicos y subidas anónimas de
cuentas desactivadas».

## Contexto

Desactivar una cuenta (`PATCH /users/{id}` con `status=disabled`, §20) corta
hoy todo lo que se autentica **como** esa cuenta: sesiones
(`auth/authenticator.go`, `ErrUserDisabled`), tokens de API
(`auth/api_token.go`) y tokens WebDAV (`webdav/token.go`). No corta lo que la
cuenta **dejó publicado**: los enlaces públicos (`/public/shares/{token}`:
metadata, descarga, navegación y subida) y los enlaces de subida anónima
(`/public/anonymous-uploads/{token}`) siguen funcionando, porque
`storage.FileService` no conoce el estado de los usuarios (`storage` no
importa `users`, por diseño).

El caso que importa es la baja de un empleado. Un enlace público es una
credencial al portador: quien tenga la URL (incluida la persona que se va)
sigue descargando datos de la organización después de la baja, y los
enlaces de subida anónima siguen llenando la cuota de una cuenta que ya nadie
gestiona. El cliente de escritorio, además, promete lo contrario en el
diálogo de desactivar.

Puntos del código que acotan el diseño:

- Todo acceso público pasa por **dos únicos puntos de resolución**:
  `FileService.ResolvePublicShare` (que usan el probe `GetPublicShare` y,
  vía `ResolvePublicShareForAccess`, la descarga, la navegación y la subida)
  y `FileService.ResolveAnonymousUploadForAccess` (que usan el probe y la
  subida anónima).
- Ya hay un precedente de inyectar en `storage` algo que respalda
  `users` sin importarlo: `QuotaResolver` + `WithQuotas` (ADR-036), que
  cumple `users.Service` de forma estructural y se cablea en
  `internal/server`.
- `Share.OwnerID` y `AnonymousUpload.OwnerID` ya están en la fila resuelta:
  no hace falta ninguna consulta extra para saber de quién es el enlace.

## Opciones consideradas

| Opción | A favor | En contra |
|---|---|---|
| A. `JOIN users` en el SQL de `shares`/`anonymous_uploads` | Atómica y sin consulta extra | Acopla el SQL de `storage` al esquema de `users` (literal `'active'` duplicado); salta la frontera de contexto por la puerta de atrás |
| B. Comprobación en cada handler público de `apiv1` | No toca `storage` | Seis handlers hoy; el siguiente que se añada puede olvidarla (falla abierto). Obliga a resolver el token dos veces |
| **C. Interfaz inyectada en `storage`, comprobada en los dos puntos de resolución** | Un solo sitio cubre todos los consumidores públicos actuales y futuros; sigue el precedente de `WithQuotas`; reversible | Una consulta por PK más por petición pública (despreciable frente a Argon2id o al streaming) |
| D. Revocar los enlaces al desactivar | Semántica simple | Irreversible: reactivar no los devuelve. `users.Disable` tendría que llamar a `storage` (dependencia al revés) |

## Decisión

1. **Opción C.** `storage` declara lo que necesita:

   ```go
   // OwnerStatusChecker dice si el propietario de un recurso publicado sigue
   // activo. Lo cumple users.Service.IsActive: storage no importa users.
   type OwnerStatusChecker interface {
       IsActive(ctx context.Context, userID string) (bool, error)
   }
   func WithOwnerStatus(c OwnerStatusChecker) FileServiceOption
   ```

   `users.Service` gana `IsActive(ctx, userID)` (lee el usuario y devuelve
   `u.IsActive()`; un usuario inexistente da `false, nil`).
   `internal/server` lo cablea junto a `WithQuotas`.

2. **Dónde se comprueba**: justo después de leer la fila por `token_hash`,
   dentro de `ResolvePublicShare` y de `ResolveAnonymousUploadForAccess`,
   **antes** del ciclo de vida, de la contraseña (no se gasta Argon2id) y de
   `IncrementDownloadCount` (no se consume una descarga).

3. **Respuesta**: propietario inactivo = el enlace **no existe**:
   `ErrShareNotFound` / `ErrAnonymousUploadNotFound` → 404 `not_found`. No se
   distingue de un token inexistente, así que no se filtra el estado de la
   cuenta. En los shares no se usa el 410 `share_revoked`, que confirmaría que
   el enlace existió.

4. **Fallar cerrado**:
   - Un error al consultar el estado → error interno (500 genérico) y no se
     sirve nada.
   - Un `FileService` con enlaces públicos o subida anónima activados pero
     **sin** `WithOwnerStatus` rechaza toda resolución pública con
     `ErrOwnerStatusUnavailable` (500). Esto es al
     contrario que `WithQuotas`, donde sin la opción no hay comprobación: aquí
     olvidarse de cablearla abriría justo el hueco que este ADR cierra. El
     helper de tests de `storage` cablea un comprobador en memoria.

5. **Reversible, sin tocar filas**: desactivar no revoca ni modifica
   `shares` ni `anonymous_uploads`. Al reactivar la cuenta, los enlaces
   vuelven a funcionar con su caducidad y sus contadores tal como estaban
   (la caducidad sigue corriendo mientras tanto). Quien quiera cortarlos para
   siempre los revoca aparte, como hoy.

6. **Alcance: solo superficies sin cuenta** (enlaces públicos y de subida
   anónima). Las comparticiones con **usuarios y grupos** (`share_type`
   `user`/`group`) de una cuenta desactivada **se mantienen** (ver «Pregunta
   resuelta»). Motivo: el riesgo que
   corrige este ADR es una credencial al portador que puede tener cualquiera,
   incluida la persona que se va. Los destinatarios de una compartición con
   usuario o grupo son cuentas activas, identificadas y bajo control del
   administrador. Cortarlas convertiría «desactivar» en «congelar los datos»,
   y el equipo perdería de golpe el acceso a carpetas de trabajo sin que
   exista todavía una herramienta para transferir la propiedad.

7. **Lo que se ve en la interfaz** (web, cliente y `docs/administracion.md`):
   «Al desactivar una cuenta se cierran sus sesiones y dejan de funcionar sus
   tokens y sus enlaces públicos y de subida anónima. Lo que compartió con
   usuarios y grupos sigue accesible. Reactivarla restaura sus enlaces.»
   Los listados del propio usuario (`GET /shares`, `GET /anonymous-uploads`)
   no cambian: esa cuenta no puede iniciar sesión para verlos.

## Pregunta resuelta (2026-10-09)

¿Se cortan también las comparticiones con usuarios y grupos de una cuenta
desactivada? **No**, como recomendaba la decisión 6. Si algún día cambia, el
mismo `OwnerStatusChecker` se comprueba en `hasShareAccessToFile`,
`hasShareAccessToDirectory`, `ListSharesWithMe` y `UploadToSharedDirectory`,
y el texto de la decisión 7 cambia. Es un cambio de unas pocas líneas, pero
de producto, no de seguridad.

## Consecuencias

- Una consulta por PK a `users` por cada petición pública. No se cachea
  (§168: el estado se comprueba en cada petición, igual que el rol en
  `RequireAdmin`), así que desactivar surte efecto de inmediato.
- La CLI (`internal/cli/helpers.go`, `backup_cmd.go`) construye su propio
  `FileService` y no sirve enlaces: no necesita la opción. La regla de la
  decisión 4 solo afecta a la resolución pública.
- Sin migración ni cambio de esquema.
- **Verificación**: tests de `storage` con SQLite real (share de archivo, de
  carpeta, con contraseña, y enlace de subida anónima de un propietario
  desactivado → `NotFound`, sin incrementar `download_count` ni
  `upload_count`; al reactivar vuelven a funcionar; error del comprobador →
  error; `FileService` sin la opción → rechaza). Tests de integración HTTP:
  los seis endpoints públicos dan 404 con el propietario desactivado.

## Fuera de alcance

- Transferir la propiedad de los datos de una cuenta que se va (encaja en la
  fase B de administración, ADR-042).
- Cortar las comparticiones con usuarios y grupos (ver «Pregunta resuelta»).
- El rol `read_only` (ADR-044).
