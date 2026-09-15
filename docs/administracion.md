# Administración: usuarios, grupos, doble factor

Todo lo de aquí se hace por CLI, siempre como el usuario de servicio
(`sudo -u nexuscloud ...` en Linux) o directamente en Windows. Referencia
rápida de cada comando en [`comandos.md`](comandos.md); esta página es la
guía de "cómo hago X" paso a paso.

## Usuarios

### Crear un usuario

```sh
nexuscloud --config config.yaml users create --username maria --password 'una-contraseña-de-verdad'
```

Sin `--password`, la pide de forma interactiva. Rol `user` por defecto —
para dar de alta a otro administrador, usa
`admin create-user --role administrator` en su lugar (ver
[`comandos.md`](comandos.md#administración-de-la-instancia-admin)).

### Ver quién tiene cuenta

```sh
$ nexuscloud --config config.yaml users list
47ac9197-2045-46a8-8609-f14e656401eb  admin                 active      admin
1cb35768-8452-4773-aa72-ea9e387ed639  maria                 active      maria
```

### Deshabilitar un usuario

```sh
nexuscloud --config config.yaml users disable maria
```

Bloquea el login inmediatamente; sus archivos y configuración no se
tocan. No existe todavía un comando para volver a habilitarlo por CLI (ver
"Lo que falta" más abajo) — mientras tanto, la API HTTP sí lo permite
(`PATCH /api/v1/users/{id}` con `{"status": "active"}`, ver
[`nexuscloud/docs/api.md`](../nexuscloud/docs/api.md)).

## Grupos

Sirven para compartir carpetas/archivos con varias personas a la vez (una
familia, un equipo) en vez de usuario por usuario. Compartir con un grupo
en sí se hace desde la interfaz web o por API (`POST /api/v1/shares` con
`share_type: "group"`) — el CLI cubre crear el grupo y meter gente dentro:

```sh
$ nexuscloud --config config.yaml users group create Familia
Grupo "Familia" creado (id=b8da3154-b428-4772-b11a-ebe2d38aacc1)

$ nexuscloud --config config.yaml users group add-member maria Familia
Usuario "maria" añadido al grupo "Familia"
```

`add-member` es idempotente: repetir la misma llamada no da error ni
duplica nada. Un nombre de grupo repetido sí da error (409/"ya existe").

## Doble factor (TOTP)

Compatible con cualquier app autenticadora estándar (Google Authenticator,
Aegis, 1Password, Authy...). El proceso es de dos pasos a propósito: el
secreto generado en `enroll` **no queda activo** hasta que confirmas un
código real generado por tu app en `verify` — así nunca puedes activar 2FA
por accidente con un secreto que nunca llegaste a registrar y quedarte
fuera de tu propia cuenta.

```sh
$ nexuscloud --config config.yaml users totp enroll --username maria
Secreto: JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R
URL (pégala en tu app autenticadora, o genera un QR con ella): otpauth://totp/NexusCloud:maria?algorithm=SHA1&digits=6&issuer=NexusCloud&period=30&secret=JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R

Todavía NO está activo. Genera un código con tu app y confirma con:
  nexuscloud users totp verify --username maria --secret JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R <código>
```

Copia esa URL en tu app autenticadora (o convierte la URL en un código QR
con cualquier generador y escanéala), espera al código de 6 dígitos que te
muestre, y confirma:

```sh
$ nexuscloud --config config.yaml users totp verify --username maria --secret JSZBMXDG3R2F65TODSSVR2EBX3ELFJ7R 450456
2FA activado para "maria": a partir de ahora el login exigirá también el código TOTP.
```

A partir de aquí, el login de "maria" (por la API, o por cualquier cliente
que la use) exige el código además de la contraseña.

### Si algo sale mal (recuperar el acceso)

```sh
nexuscloud --config config.yaml users totp disable --username maria
```

Desactiva 2FA para ese usuario sin necesitar ningún código — la vía de
recuperación si se perdió el teléfono con la app, o el secreto se copió
mal. En un servidor headless de un solo administrador, esto es
importante: no hay ninguna otra forma de deshacerlo.

## Lo que falta por CLI (backlog documentado, no bloquea el uso diario)

Estas operaciones existen en la API HTTP pero todavía no tienen comando
equivalente — ninguna es necesaria para el uso diario de una instalación
de un solo administrador, por eso quedaron fuera de la primera vuelta:

- Reactivar/editar/borrar un usuario ya creado (`PATCH`/`DELETE /users/{id}`).
- Gestionar sesiones activas de OTRO usuario (revocarlas a distancia).
- Ver/restaurar la papelera o el historial de versiones de OTRO usuario.
- Invitaciones (crear/listar/revocar) — la vía de alta directa
  (`admin create-user` / `users create`) cubre el mismo caso de uso.
- Consultar el registro de auditoría (`GET /api/v1/audit`).

Para cualquiera de estas, usa la API directamente — ver
[`nexuscloud/docs/api.md`](../nexuscloud/docs/api.md).
