# Modelo de amenazas

Análisis por actor (§166), reflejando el estado real de la Fase 1 — qué está mitigado hoy y qué queda pendiente para fases posteriores. Ver [docs/security.md](../security.md) para el detalle de cada control.

## Atacante externo no autenticado

**Puede intentar**: fuerza bruta de login, enumeración de usuarios, path traversal, explotar la API sin sesión, DoS.

**Mitigado hoy**: rate limiting específico en `/auth/login` (§27); mensaje de error idéntico ante usuario inexistente o contraseña incorrecta; todas las rutas de archivos/usuarios/invitaciones (salvo login y redeem) exigen sesión válida; `SafeJoin` neutraliza path traversal en cualquier ruta de archivo; CORS con allowlist explícita (nunca `*`); cabeceras de seguridad estándar.

**Pendiente**: protección DoS de capa de red (queda fuera del alcance de la aplicación — es responsabilidad del reverse proxy/firewall, ver `docs/deployment.md`); CAPTCHA o backoff progresivo más allá del rate limit fijo actual.

## Usuario autenticado malicioso

**Puede intentar**: acceder a archivos de otro usuario adivinando/enumerando IDs (IDOR), escalar a rol de administrador, abusar de invitaciones.

**Mitigado hoy**: IDs de archivo/sesión/invitación son UUID v4 aleatorios, no enumerables; `FileService.Download`/`Delete` comprueban `OwnerID` explícitamente; `RevokeSession` exige coincidencia de `user_id` en la propia query; `RequireAdmin` comprueba el rol en base de datos en cada petición, no un claim del cliente; las invitaciones tienen límite de usos y expiración, y se pueden revocar.

**Pendiente**: no hay todavía límites de cuota aplicados en la ruta de subida (el campo `quota_bytes` existe en el modelo de datos pero no se aplica activamente — Fase 2); no hay sharing todavía, así que el modelo de "otro usuario" se limita a intentos de acceso directo, no a abuso de permisos compartidos.

## Sesión/cuenta comprometida (credential theft)

**Puede intentar**: usar una sesión o contraseña robada.

**Mitigado hoy**: el usuario legítimo puede listar (`GET /auth/sessions`) y revocar (`DELETE /auth/sessions/{id}`) cualquier sesión activa, incluida una robada, en cuanto lo detecta (§26); TOTP disponible como segundo factor; contraseñas nunca se registran en logs ni se devuelven en ninguna respuesta.

**Pendiente**: no hay todavía notificación proactiva al usuario de "nuevo inicio de sesión desde IP/dispositivo desconocido" (requiere el sistema de notificaciones de fases posteriores); no hay bloqueo automático de cuenta tras N intentos fallidos más allá del rate limiting por IP (un atacante distribuido en IPs podría seguir intentando, aunque cada IP individual queda limitada).

## Administrador comprometido

**Puede intentar**: crear/eliminar usuarios, revocar invitaciones ajenas, leer el log de auditoría para borrar su rastro.

**Mitigado hoy**: toda acción administrativa relevante queda registrada en `audit_events` con actor, IP y timestamp; un admin no puede eliminarse a sí mismo vía API (previene bloqueo accidental, no es un control de seguridad contra un admin malicioso real).

**Pendiente**: §126 pide reautenticación reforzada para acciones administrativas críticas (eliminar usuario, cambiar configuración de seguridad, desactivar 2FA) — hoy solo está documentada como política, no aplicada técnicamente; el log de auditoría no es criptográficamente inmutable (§177) — un admin con acceso a la base de datos podría alterarlo. Ambos quedan para una fase de seguridad avanzada.

## Malware local / equipo comprometido del usuario

**Puede intentar**: robar el token de sesión almacenado localmente por un cliente, leer archivos sincronizados en claro.

**Mitigado hoy**: fuera del alcance de esta fase (no hay clientes de escritorio/móvil todavía, solo API). Cuando lleguen (Fase 3-4), el ecosistema ya tiene un patrón validado en NexusKeys: `flutter_secure_storage` respaldado por Keystore/Credential Manager del SO, nunca texto plano.

**Pendiente**: cifrado extremo a extremo (E2EE, §29) explícitamente no es requisito de la Fase 1; se documenta la arquitectura como preparada para añadirlo sin que sea obligatorio ahora mismo.

## Ransomware

**Puede intentar**: cifrar los archivos de un usuario y pedir rescate.

**Mitigado hoy**: nada específico todavía — no hay versionado ni papelera implementados en Fase 1 (esa es precisamente la primera línea de defensa contra ransomware, §128).

**Pendiente**: es una prioridad explícita de Fase 2 (versionado/papelera) y Fase 5 (backups, snapshots). Hasta entonces, un ransomware con las credenciales de un usuario puede sobrescribir sus archivos sin posibilidad de recuperación vía NexusCloud — se documenta este gap explícitamente para que no se asuma protección donde no la hay.

## Robo del dispositivo servidor

**Puede intentar**: acceder directamente al disco si el servidor (p.ej. una Raspberry Pi doméstica) es robado físicamente.

**Mitigado hoy**: fuera del alcance de la aplicación — cifrado de disco es responsabilidad del sistema operativo/filesystem subyacente (§29, "Cifrado del disco"), documentado explícitamente como una capa distinta de "Cifrado de NexusCloud".

**Pendiente**: NexusCloud no implementa cifrado de datos en reposo a nivel de aplicación en esta fase.

## Filtración de backup

No aplica todavía: el Backup Manager es una funcionalidad de Fase 5. Cuando exista, §173 ya exige que los backups puedan cifrarse.

## Ataques internos (red local)

**Puede intentar**: un dispositivo en la misma LAN intercepta tráfico HTTP sin cifrar.

**Mitigado hoy**: NexusCloud permite configurar TLS directamente (`server.tlsCertFile`/`tlsKeyFile`) incluso para uso puramente en LAN, si el administrador lo desea (§66); por defecto no lo exige, ya que muchas instalaciones domésticas confían en el perímetro de su propia red.

**Pendiente**: no hay ningún mecanismo que fuerce HTTPS en LAN — es una decisión consciente del administrador, documentada en `docs/deployment.md`.
