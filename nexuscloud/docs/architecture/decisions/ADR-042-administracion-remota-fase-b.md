# ADR-042: Administración remota, fase B (marco de seguridad común)

## Estado

Aceptado (2026-10-09), con **B3 (configuración) aplazado**: ver «Preguntas
resueltas». Ticket del backlog:
«Administración desde el cliente, fase B» (f01f71c7). Se apoya en ADR-004
(sesiones), ADR-015..029 (backups), ADR-018..022 (RAID y snapshots),
ADR-037 (tokens de API), ADR-043 y ADR-044.

## Contexto

La fase A de la sección «Administración» del cliente usó la API que ya
existía (usuarios, grupos, invitaciones, auditoría, discos, cola de
miniaturas). El usuario eligió para la fase B, el 2026-09-28: cuentas
completas, backups y almacenamiento (con alertas, §58) y configuración del
servidor. El dashboard de §56/§57 queda fuera.

Cada endpoint nuevo amplía **lo que puede hacer en remoto una sesión de
administrador robada**. Hoy esa sesión ya puede crear, editar, desactivar y
borrar usuarios. La fase B le añadiría restaurar backups, tocar el
almacenamiento y cambiar la configuración. El modelo de seguridad común es,
por tanto, la decisión principal de este ADR. Los detalles de cada bloque
van en sub-tickets que se entregan por separado.

Hechos del código que acotan el diseño:

- **No hay reautenticación** de ningún tipo. `sessions` no tiene ninguna
  columna que permita algo como «sudo» (migración 0001).
- **Revocación masiva**: `auth.SessionRepository.RevokeAllForUser` existe,
  pero nadie la usa. Los tokens de API y los de WebDAV no tienen ningún
  equivalente.
- **Roles**: `user_roles` es N:M, pero cada alta asigna un solo rol. No
  existe `RemoveRole`. `super_admin` y `administrator` son hoy
  indistinguibles (`IsAdmin` los trata igual), así que un `administrator`
  puede crear un `super_admin`. `userResponse` no incluye el rol.
- **Grupos**: no se puede quitar un miembro, ni renombrar ni borrar un
  grupo. Borrar un grupo arrastra por FK (`ON DELETE CASCADE`) sus
  membresías **y las comparticiones hechas al grupo**.
- **Backups**: `backup.Manager.Run` es síncrono y no tiene exclusión mutua
  con el bucle automático ni con la CLI. `Run`, `Restore` y `RestoreToPool`
  reciben **rutas o URLs arbitrarias** (`DestinationPath`, http(s)://). La
  passphrase de cifrado y el token remoto solo viven en variables de
  entorno.
- **Pools**: `storage pool add` recibe una ruta arbitraria del sistema de
  archivos y la crea.
- **Despliegue**: la unidad systemd usa `ProtectSystem=strict` con
  `ReadWritePaths=/var/lib/nexuscloud`. **El servicio no puede escribir
  `/etc/nexuscloud/config.yaml`** (instalado 0640, propiedad del usuario
  del servicio pero en un FS de solo lectura para él), ni crear pools o
  backups fuera de su directorio de datos sin que el administrador edite la
  unidad. El servidor tampoco recarga la configuración en caliente.
- **Alertas (§58)**: no existe nada todavía.
- **Auditoría**: `GET /audit` serializa `audit.Event` sin etiquetas JSON
  (`ID`, `EventType`, `ActorUserID`…).
- **Borrar un usuario** elimina sus metadatos, pero su contenido físico se
  queda huérfano en el pool.
- **El cliente no sabe qué servidor tiene enfrente**: no hay versión ni
  capacidades en la API. Por eso no puede ofrecer «Solo lectura» con
  seguridad (ADR-044) ni adaptar el texto de «Desactivar» (ADR-043).

## Decisión

### Marco común (aplica a B1, B2 y B3)

1. **Partir la fase B en tres entregas independientes**, en este orden de
   menor a mayor riesgo: **B1 cuentas**, **B2 backups y almacenamiento**,
   **B3 configuración**. Cada una lleva su propio sub-ticket y puede
   publicarse sin las demás.

2. **Reautenticación reforzada (§126), modo «sudo» por sesión**:
   - `POST /auth/reauthenticate` con la contraseña (y el código TOTP si la
     cuenta lo tiene) marca la sesión actual con `reauthenticated_at`
     (columna nueva en `sessions`, en los tres motores). Va bajo el rate
     limit de login.
   - Un middleware `RequireRecentReauth` exige que hayan pasado **menos de
     10 minutos** desde entonces. Si no, responde 403 `reauth_required` y
     el cliente pide la contraseña y reintenta.
   - **Los tokens de API no pueden pasar este control**: no hay sesión
     interactiva detrás. Las acciones protegidas son, por diseño, de sesión
     interactiva o de CLI local (headless-first: la CLI sigue pudiéndolo
     todo).
   - Acciones protegidas: borrar usuario (ya existe; pasa a exigirlo),
     restablecer contraseña, quitar 2FA o passkeys de otra cuenta, cambiar
     un rol hacia o desde `administrator`/`super_admin`, borrar un grupo,
     restaurar un backup, cambiar la política o quitar un pool, y cualquier
     escritura de configuración.

3. **Auditoría de cada acción** con actor, objetivo e IP, y eventos nuevos
   donde no existan (`role_changed`, `password_reset`, `totp_removed`,
   `sessions_revoked`, `group_member_removed`, `group_renamed`,
   `group_deleted`, `backup_started`, `backup_restored`,
   `pool_policy_changed`, `config_changed`, que ya está declarado y sin
   uso).

4. **Envolver los servicios de la CLI, no duplicarlos**. Si una operación
   no existe en un servicio (p. ej. quitar un miembro), se añade al
   servicio y la usan **a la vez** la CLI y la API.

5. **Secure by default**: B1 está activa siempre, porque es autenticada,
   solo para administradores y no tiene coste especial. **B2 y B3** van
   detrás de `admin.remoteManagement` (**false** por defecto). Desactivado,
   sus rutas ni se registran (404).

6. **Capacidades del servidor**: `GET /users/me` añade `server_version` y
   `capabilities` (lista de cadenas: `read_only_role`,
   `disabled_owner_links`, `remote_backups`…). El cliente solo ofrece una
   función si el servidor la anuncia. Solo lo ve un usuario autenticado.

7. **Contrato de auditoría**: `GET /audit` pasa a etiquetas JSON
   snake_case (`id`, `occurred_at`, `actor_user_id`, `event_type`,
   `target_type`, `target_id`, `ip`, `metadata`). El único consumidor es
   nuestro cliente, y se actualiza en la misma entrega (B1).

### B1: cuentas

8. **Un solo rol por usuario**. `users.Service.SetRole(actor, target,
   role)` reemplaza el rol en una transacción y valida con `ValidateRole`.
   `userResponse` añade `role`. Reglas, todas en el dominio:
   - nadie cambia su propio rol;
   - solo un `super_admin` concede, quita o modifica `super_admin`, y un
     `administrator` no puede tocar a un `super_admin` (ni editarlo, ni
     desactivarlo, ni borrarlo). Esta es la **primera diferencia real**
     entre los dos roles;
   - nunca se queda la instancia sin ningún `super_admin` activo, sea por
     cambio de rol, por desactivación o por borrado;
   - **pasar una cuenta a `read_only`** (dependencia de ADR-044) **se
     rechaza con 409** mientras tenga enlaces públicos, enlaces de subida
     anónima o comparticiones con permiso de subida vigentes. La respuesta
     dice cuántos hay de cada tipo, y el administrador los revoca primero.
     Así no hay efectos ocultos.

9. **Restablecer contraseña**: el administrador fija una contraseña nueva
   (mismas reglas que al crear una cuenta). Después se revocan **todas**
   las sesiones, los tokens de API y los tokens WebDAV de esa cuenta (se
   añade `RevokeAllForUser` a los dos repositorios de tokens). Exige
   reautenticación.

10. **Quitar 2FA o passkeys de otra cuenta**: envuelve lo que ya hacen
    `users totp disable` y `users webauthn revoke`, revoca sus sesiones y
    exige reautenticación.

11. **Grupos**: listar miembros, quitar miembro, renombrar y borrar. Borrar
    un grupo exige reautenticación y una confirmación con el **número de
    comparticiones que se perderán** (por la cascada de la FK). Sin cambio
    de esquema.

12. **Sesiones, tokens y comparticiones de otros**: listar y revocar.
    Nunca se muestran valores de token, solo metadatos.

### B2: backups y almacenamiento (detrás de `admin.remoteManagement`)

13. **Nunca rutas ni URLs desde HTTP**. Los backups remotos usan solo el
    destino que ya está en la configuración (`cfg.BackupsDir()` o
    `backup.remoteDestination`). `Restore` a una ruta arbitraria sigue
    siendo **solo de CLI**. En remoto solo existe `RestoreToPool`, con su
    regla actual de pool inactivo (ADR-025). Así se evita escribir en
    cualquier sitio del disco y hacer SSRF con una sesión robada.

14. **Backups como trabajos asíncronos**: `POST /admin/backups` responde
    202 con el job, y el cliente consulta su estado. **Un solo backup a la
    vez en toda la instancia**: la comprobación se hace en la BD (un job
    `running` reciente bloquea uno nuevo), para que cubra también a la CLI,
    que es otro proceso. La goroutine lleva su propio `recover()`.

15. **Passphrase y token remoto: nunca por HTTP**. Un backup cifrado o
    remoto solo se puede lanzar, verificar o restaurar si el servidor ya
    tiene `NEXUSCLOUD_BACKUP_PASSPHRASE` o `NEXUSCLOUD_BACKUP_REMOTE_TOKEN`
    en su entorno. Si no los tiene: 409 `secret_not_configured`.

16. **Pools**: en remoto, listar y cambiar la política de uso y de backup.
    **Añadir y quitar pools sigue siendo solo de CLI**: implica rutas del
    sistema de archivos y, con la unidad systemd endurecida, editar
    `ReadWritePaths`, algo que el servidor no puede hacer por sí mismo.

17. **RAID y snapshots**: solo lectura (ya existen `diskinfo`, `raidinfo` y
    `snapshotinfo`).

18. **Alertas (§58) calculadas al consultar, sin persistencia**:
    `GET /admin/health` devuelve una lista de avisos (disco por encima de un
    umbral, RAID degradado, último backup fallido o demasiado antiguo,
    miniaturas fallidas). No hay notificaciones push ni correo: no existe
    canal de envío, y añadirlo es otra decisión.

### B3: configuración (APLAZADO, ver «Preguntas resueltas»)

El diseño se deja escrito para cuando se retome; hoy no se implementa nada
de este bloque.

19. **No se reescribe `config.yaml`**: con `ProtectSystem=strict` el
    servicio no puede, y no debe poder (un proceso comprometido no tiene
    que poder reescribir su propia configuración). En su lugar, un fichero
    de **overrides** en el directorio de datos
    (`<dataDir>/config/admin-overrides.yaml`):
    - **lista blanca de claves**, aplicada al **escribir y otra vez al
      cargar**, de modo que aunque se manipule el fichero solo entran las
      claves permitidas. Candidatas: retención de papelera y versiones,
      cuota global por defecto, `sharing.publicLinksEnabled`,
      `sharing.anonymousUploadEnabled`, `search.enabled`,
      `thumbnails.enabled`, límites de rate limit. **Nunca**: rutas, BD/DSN,
      TLS, `trustedProxies`, CORS, `web.enabled`, `webdav.*`,
      `admin.remoteManagement` (el propio interruptor) ni secretos;
    - escritura atómica (temporal + rename) y `config.Validate` sobre la
      configuración resultante antes de guardarla;
    - **sin recarga en caliente**: la respuesta y `GET /admin/config`
      indican «pendiente de reinicio». Prioridad al arrancar: `config.yaml`,
      después overrides y después variables de entorno (las de entorno
      siguen ganando);
    - `config_changed` con la clave, el valor anterior y el nuevo, y
      reautenticación obligatoria.

## Preguntas resueltas (2026-10-09)

1. **Partir en B1/B2/B3**: sí, con un sub-ticket independiente para cada
   uno.
2. **Restablecer contraseña**: el administrador fija una contraseña nueva y
   se revocan todas las sesiones y tokens de la cuenta (decisión 9). El
   enlace de un solo uso con cambio obligatorio queda fuera.
3. **Pasar a `read_only` con cosas publicadas**: 409 hasta que se revoquen
   (decisión 8).
4. **B3 se aplaza entero**. Un segundo origen de configuración es otra
   fuente de errores, y es la parte de menos uso. Las decisiones 19 y 5 (en
   lo que toca a B3) quedan escritas para cuando se retome; hasta entonces,
   la configuración solo se cambia en el servidor (`config.yaml` y variables
   de entorno).
5. **Contenido huérfano de usuarios borrados**: ticket aparte, fuera de la
   fase B.

## Consecuencias

- Migración en los **tres motores** para `sessions.reauthenticated_at`
  (B1). B2 y B3 no tocan el esquema.
- Una sesión robada sigue siendo grave, pero **las acciones más dañinas
  exigen conocer la contraseña** en los 10 minutos anteriores, y ninguna
  permite escribir en rutas arbitrarias, apuntar a URLs arbitrarias ni
  extraer secretos.
- La CLI sigue siendo la vía completa (headless-first). La API remota es un
  subconjunto deliberado.
- `GET /audit` cambia de forma, y el cliente se actualiza en la misma
  entrega.
- **Verificación** por entrega: tests de dominio con SQLite real (reglas de
  roles, último `super_admin`, `RevokeAllForUser`), tests HTTP de cada ruta
  (incluidos `reauth_required`, el rechazo de tokens de API y 404 con
  `admin.remoteManagement=false`), la función de aptitud de ADR-044 sigue
  en verde (las rutas nuevas van dentro de `RequireWritable`), y la
  migración se aplica contra MySQL 8 y PostgreSQL 16 reales cuando el
  usuario lo pida.

## Fuera de alcance

- B3 (configuración remota), aplazado.
- Purgar el contenido físico de un usuario borrado (ticket aparte).
- Dashboard y monitorización (§56/§57): CPU, RAM, I/O, Prometheus.
- Notificaciones de alertas (correo, push).
- Roles personalizados y permisos granulares (§22).
- Recarga de configuración en caliente.
- Transferir la propiedad de los datos de una cuenta a otra.
