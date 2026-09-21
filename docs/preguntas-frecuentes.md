# Preguntas frecuentes

**¿NexusCloud depende de Docker?**
No. Docker/Compose son un método de conveniencia, no un requisito — una
instalación nativa (`install.sh`/`install.bat`) es igual de soportada, y
la que se recomienda para un servidor headless sin Docker corriendo
permanentemente. Ver [`primeros-pasos.md`](primeros-pasos.md).

**¿Necesito la interfaz web para administrar NexusCloud?**
No, absolutamente todo se puede hacer por CLI — pensado precisamente para
un servidor Linux sin entorno gráfico. La web es opcional, viene
desactivada por defecto, y se puede añadir o quitar después sin
reinstalar desde cero. Ver [`comandos.md`](comandos.md) y
[`mantenimiento.md`](mantenimiento.md#añadir-o-quitar-la-interfaz-web).

**¿Puedo usar MySQL/MariaDB o PostgreSQL en vez de SQLite?**
Sí, los tres son soportados por igual (`database.driver` en `config.yaml`:
`sqlite`, `mysql`/`mariadb`, o `postgres`). SQLite es el valor por defecto
porque no necesita ningún servicio de base de datos aparte — para una
instalación con más de un usuario concurrente activo, un motor cliente/
servidor puede ir mejor.

**¿Qué pasa si borro un archivo por error?**
Va a la papelera (recuperable durante `trash.retentionDays`, 30 días por
defecto), salvo que uses `--permanent` explícitamente. Lo mismo aplica
subiendo por CLI, por la web o desde el cliente de escritorio.

**¿Y si sobrescribo un archivo con una versión peor?**
El versionado (activado por defecto, `versioning.enabled`) conserva
versiones anteriores del mismo archivo — puedes restaurar cualquiera. Se
poda automáticamente por cantidad, antigüedad o espacio total según
`versioning.maxVersionsPerFile`/`maxVersionAgeDays`/`maxVersionsTotalSizeBytes`.

**¿Cómo comparto una carpeta con otra persona, o con varias a la vez?**
Con una persona: comparte directamente desde la web o el cliente de
escritorio (o por API, `POST /api/v1/shares`). Con varias a la vez: crea
un grupo (`nexuscloud users group create`), añade a cada persona
(`users group add-member`), y comparte con el grupo en vez de con cada
usuario por separado. Ver [`administracion.md`](administracion.md#grupos).

**¿Es seguro activar el registro público de usuarios?**
Viene desactivado por defecto (`security.publicRegistrationEnabled: false`)
a propósito — en una instancia personal/familiar, dar de alta a cada
persona tú mismo (`users create`) o por invitación es lo recomendado.
Actívalo solo si sabes lo que implica (cualquiera con acceso a la URL
podría crearse una cuenta).

**¿Necesito HTTPS?**
Si NexusCloud solo va a vivir dentro de tu red local, no es estrictamente
necesario (por eso `doctor` solo da un `WARNING`, no un `FAIL`, sin TLS
configurado). Si vas a exponerlo a Internet, sí — configura
`server.tlsCertFile`/`tlsKeyFile`, o pon un reverse proxy (Caddy, nginx,
Traefik) por delante que termine TLS.

**¿Cuánto tarda un backup? ¿Puedo hacerlo mientras la gente usa NexusCloud?**
Sí, no bloquea el uso normal — el Backup Manager lee los archivos activos
sin necesidad de parar el servicio. El tiempo depende del volumen de
datos y de si usas `--incremental` (mucho más rápido en backups
sucesivos, ya que los ficheros sin cambios se enlazan en vez de
recopiarse). Ver [`backup-y-recuperacion.md`](backup-y-recuperacion.md).

**¿Puedo mover los datos a otro disco más adelante?**
Sí. Cada área de almacenamiento (base de datos, caché, miniaturas,
versionado, temporales, logs, backups, config) puede vivir en un disco
distinto sin tocar código —
`storage.databaseDir`/`storage.cacheDir`/etc. en `config.yaml`, o las
variables `NEXUSCLOUD_DATABASE_DIR`/`NEXUSCLOUD_CACHE_DIR`/etc. Ver
`nexuscloud/docs/architecture/storage-evolution-plan.md`.

**¿Qué diferencia hay entre esta carpeta (`docs/`) y `nexuscloud/docs/`?**
Esta (`docs/` en la raíz) es para ti, la persona que instala y administra
NexusCloud — comandos, cómo funciona cada cosa, sin necesitar leer código.
`nexuscloud/docs/` es para quien vaya a modificar el propio NexusCloud —
arquitectura, decisiones de diseño (ADRs), referencia de la API HTTP a
nivel de esquema.

**¿Puedo limitar cuánto espacio usa cada persona?**
Sí, con cuotas de almacenamiento: por usuario (`users edit maria --quota
100GB`), por grupo (`users group edit Familia --quota 500GB`, es un límite
por miembro) o una global (`storage.defaultQuotaBytes` en `config.yaml`). Por
defecto no hay ninguna. Cuentan los archivos, la papelera y las versiones
anteriores, y lo que otras personas suben a una carpeta tuya cuenta contra
la tuya. Ver [`administracion.md`](administracion.md#cuotas-de-almacenamiento).

**Me dice «No hay espacio suficiente: has alcanzado tu cuota», pero borré
archivos. ¿Por qué?**
Borrar mueve a la papelera, y la papelera también cuenta (igual que las
versiones anteriores de un archivo que sobrescribiste). El espacio se libera
al vaciar la papelera o al pasar su tiempo de retención. Puedes ver en qué se
va el espacio en la barra «Almacenamiento» de la web (pasa el ratón por encima)
o con `nexuscloud users quota <usuario> --detail`.

**No encuentro respuesta a mi problema aquí.**
Revisa [`mantenimiento.md`](mantenimiento.md#problemas-frecuentes) para
fallos concretos (servicio no arranca, `doctor` en rojo, contraseña
olvidada, 2FA bloqueado). Si es algo del propio código o arquitectura,
`nexuscloud/docs/` tiene el detalle técnico completo.
