# ADR-044: Aplicar el rol `read_only` en el servidor

## Estado

Aceptado (2026-10-09). Concreta el rol «Read
Only» de §22 y complementa ADR-004 (sesiones), ADR-034 (WebDAV) y ADR-037
(tokens de API). Ticket del backlog: «Aplicar el rol read_only en el
servidor (hoy no restringe nada)».

## Contexto

§22 enumera cuatro roles iniciales y la migración 0001 los siembra, pero
`users.RoleReadOnly` solo está declarado (`internal/users/user.go`): ningún
código lo comprueba. Un usuario creado con ese rol puede subir, borrar,
mover, compartir y publicar enlaces igual que un `user`. El cliente de
escritorio ofrece «Solo lectura» al crear usuarios e invitaciones, así que el
administrador cree que ha limitado una cuenta y no es así: es un **control de
seguridad anunciado que no existe**.

Hallazgos de la revisión del código:

- `RequireAdmin` ya comprueba el rol en cada petición contra la BD (§168).
  No existe ningún otro control por rol.
- WebDAV ya tiene un modo de solo lectura **global** (`webdav.readOnly`,
  `isReadMethod` en `internal/webdav/handler.go`: solo OPTIONS, GET, HEAD y
  PROPFIND). `internal/webdav` ya importa `users`.
- Los tokens de API (ADR-037) autentican **como** el usuario, con alcance
  todo-o-nada. Cualquier control sobre el usuario los cubre sin hacer nada
  más.
- **`role` no se valida** en `POST /users` ni en `POST /invitations`. En
  `CreateUser` es peor que un 400 que falta: `users.Service.CreateUser`
  inserta el usuario y **después** `AssignRole`, que falla por la FK de
  `user_roles.role_id`. El resultado es un 500 y **un usuario creado sin
  ningún rol**. En invitaciones, la FK de `invitations.role_id` lo convierte
  en un 500.
- `user_roles` es N:M, pero hoy cada alta asigna exactamente un rol y
  ninguna API cambia roles (llegará con la fase B, ADR-042).

## Opciones consideradas

| Opción | A favor | En contra |
|---|---|---|
| A. Comprobar en `FileService` (inyectado, como ADR-043) | Un punto único de acceso a archivos | El rol es del **actor** (quien hace la petición), no del **dueño de los datos**. `FileService` no sabe quién actúa: la CLI de administración y el restore de backups escriben en árboles de usuarios y quedarían bloqueados. No cubre `POST /shares` ni los enlaces de subida anónima, que no son escrituras de archivos |
| B. Middleware sobre una lista de rutas «que escriben» | Simple | Falla abierto: cada ruta nueva hay que acordarse de meterla en la lista |
| **C. Denegar por método (todo lo que no sea GET, HEAD u OPTIONS) en REST y WebDAV, con una lista corta de excepciones de autoservicio** | Falla cerrado: una ruta nueva que escribe queda bloqueada sin hacer nada. Reutiliza `isReadMethod` en WebDAV. Coste cero en lecturas | Hay que mantener la lista de excepciones, comprobada por un test sobre el router (decisión 5) |

## Decisión

1. **Qué es `read_only`**: una cuenta que **puede leer y no puede cambiar
   datos ni publicarlos**. Puede:
   - leer su árbol: listar, descargar, versiones, miniaturas, búsqueda,
     papelera y actividad;
   - leer lo que le comparten: `GET /shares`, `GET /shared-directories/{id}`
     y `GET /files/{id}` a través de una compartición;
   - gestionar su propia cuenta: cerrar sesión, revocar sesiones, TOTP,
     passkeys, tokens de API y WebDAV, y favoritos (metadatos suyos, no
     datos de archivos). Desde ADR-042 también `POST /auth/reauthenticate`
     (modo «sudo»): solo marca su propia sesión, no cambia ningún dato;
   - revocar sus propios enlaces y comparticiones (`DELETE /shares/{id}`,
     `DELETE /anonymous-uploads/{id}`): reducir lo que está expuesto nunca es
     una escritura peligrosa.

   No puede subir, crear carpetas, borrar, mover, restaurar (papelera o
   versiones), crear comparticiones o enlaces, crear enlaces de subida
   anónima **ni subir a carpetas que otro le compartió con permiso de
   subida** (ver «Preguntas resueltas»). El rol lo fija el
   administrador como techo: el permiso que dé un propietario no lo supera.

2. **Dónde (REST)**: el grupo autenticado de `internal/api/v1/router.go` se
   parte en dos:
   - **autoservicio de cuenta**: la lista cerrada de rutas de la decisión 1
     que escriben y están permitidas, registradas sin el guardia;
   - **todo lo demás**, incluido el subgrupo de administración, bajo un
     middleware nuevo, `RequireWritable`. Solo actúa en métodos que no sean
     GET, HEAD u OPTIONS: consulta `users.Service.IsReadOnly(ctx, userID)`
     (= `HasRole(read_only)`, en cada petición y sin caché, §168) y responde
     **403 `read_only_account`** («Tu cuenta es de solo lectura.»). Las
     lecturas no hacen ninguna consulta extra.

   Una ruta nueva va por defecto al segundo grupo: si escribe, ya está
   bloqueada para `read_only`.

3. **Dónde (WebDAV)**: `Handler.ServeHTTP` amplía la condición actual a
   `!isReadMethod(m) && (opts.ReadOnly || IsReadOnly(usuario))`. Mismo 403 y
   misma lista de métodos que el modo global. La consulta solo se hace con
   métodos que escriben.

4. **Tokens de API**: no hacen falta cambios. `RequireAuth` deja en el
   contexto el usuario dueño del token y `RequireWritable` se aplica igual.
   Un token de un usuario `read_only` es de solo lectura.

5. **Fallar cerrado**:
   - Un error al consultar el rol → 500 genérico y no se escribe nada.
   - Si un usuario tiene `read_only` junto a otro rol, **gana `read_only`**
     (la denegación prevalece). Esto no contradice la regla de «gana el más
     permisivo» de ADR-035/036: aquella combina **concesiones** que se
     solapan, y esto es una **restricción**.
   - **Test de función de aptitud**
     (`tests/integration/read_only_role_test.go`,
     `TestReadOnlyAccountCannotWriteThroughAnyRoute`): recorre con
     `chi.Walk` el router real del servidor y, para cada ruta autenticada
     con un método que escribe y que no esté en la lista de autoservicio,
     comprueba que un usuario `read_only` recibe 403 `read_only_account`.
     Una ruta nueva mal colocada rompe el CI (comprobado quitando el
     guardia: el test falla en todas las rutas de datos).

6. **Validar `role`** en el dominio, no en el handler, para cubrir API, CLI
   e invitaciones a la vez:
   - `users.ValidateRole(role)` acepta solo los cuatro roles conocidos
     (vacío = `user`) y devuelve `users.ErrInvalidRole`.
   - `users.Service.CreateUser` valida **antes** de insertar. Se acaba el
     usuario huérfano sin rol.
   - `auth.InvitationService` valida al crear la invitación.
   - Los handlers traducen `ErrInvalidRole` a 400 `invalid_role`. La CLI
     (`users create --role`, `invitations create --role`) muestra el mismo
     error.

7. **Clientes**: el cliente de escritorio y la web pueden volver a ofrecer
   «Solo lectura» cuando esto esté desplegado. Mientras tanto, el cliente lo
   retira de los selectores (lo hace el agente frontend). La interfaz oculta o
   deshabilita las acciones de escritura para un `read_only`, pero la garantía
   es solo la del servidor: la interfaz es una comodidad.

## Preguntas resueltas (2026-10-09)

1. **Subir a una carpeta compartida con permiso de subida**: un `read_only`
   **no** puede (el rol es un techo que fija el administrador).
2. **Favoritos**: **sí** se permiten (son metadatos de la propia cuenta y no
   tocan datos de archivos).

## Consecuencias

- Una consulta (`HasRole`, índice por PK) por cada petición que escribe. Las
  lecturas, que son la mayoría, no pagan nada.
- Un `read_only` tiene un árbol propio que **solo** puede llenar la CLI de
  administración (`FileService` no se toca, decisión A descartada). En la
  práctica es una cuenta para **consumir lo que otros le comparten**.
- Sin migración ni cambio de esquema: el rol ya está sembrado.
- **Dependencia con la fase B (ADR-042)**: cuando exista el cambio de rol, al
  pasar una cuenta a `read_only` hay que decidir qué pasa con lo que ya
  publicó (enlaces, enlaces de subida anónima y comparticiones con subida):
  revocarlo, o negarse a cambiar el rol mientras exista. Hoy no puede pasar,
  porque el rol solo se fija al crear la cuenta.
- **Verificación**: tests de `users` (`ValidateRole`, `CreateUser` con un rol
  desconocido no deja ningún usuario), test del router con `chi.Walk`, tests
  HTTP de cada superficie (REST, subida a carpeta compartida, creación de
  enlaces públicos y de subida anónima, token de API) y tests de WebDAV (PUT,
  DELETE, MKCOL y MOVE → 403; PROPFIND y GET → 200) con un usuario
  `read_only`.

## Fuera de alcance

- Roles personalizados y permisos granulares (§22 «Permite crear roles
  personalizados»): esto aplica un rol fijo, no un RBAC general.
- Cambiar el rol de una cuenta (fase B, ADR-042).
- **Hallazgo aparte**: un `administrator` puede crear un `super_admin` por
  `POST /users` o por invitación. Hoy no hay diferencia de privilegios entre
  los dos (`IsAdmin` los trata igual), así que no es una escalada efectiva,
  pero ADR-042 debería fijar que solo un `super_admin` concede
  `super_admin`.
