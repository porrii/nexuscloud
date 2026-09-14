# ADR-022: Detección de Snapshots en Linux vía Btrfs (`btrfs subvolume list`)

## Estado

Aceptado.

## Contexto

El Slice 8 ([ADR-021](ADR-021-snapshot-zfs.md)) cubrió ZFS en Linux y dejó Btrfs explícitamente pendiente: un snapshot Btrfs es un subvolumen con la propiedad de solo lectura, no un tipo de objeto propio como en ZFS, con un modelo y un formato de salida completamente distintos. Este slice cierra ese hueco y, con él, el pilar Snapshots completo (Windows/VSS + Linux/ZFS + Linux/Btrfs), a la par de cobertura con RAID (Linux+Windows).

**Mismo nivel de honestidad que ADR-021, con una capa más de incertidumbre**: ni el contenedor Docker de esta sesión (`which btrfs` confirmó su ausencia, sin ningún montaje Btrfs tampoco) ni la máquina de desarrollo (Windows) permiten probar esto contra un sistema real. A diferencia de ZFS -- un único comando global (`zfs list`) con banderas (`-H -p`) pensadas explícitamente para parseo -- Btrfs encadena tres suposiciones, ninguna contrastable aquí:

1. **`btrfs subvolume list` exige la RUTA de un filesystem Btrfs ya montado**, a diferencia de `zfs list`, que opera globalmente sobre todos los pools importados. Hace falta descubrir los montajes Btrfs primero.
2. **No existe un modo "parseable"** como el `-H -p` de ZFS: la salida es texto separado por espacios pensado para humanos (`ID 261 gen 100 cgen 98 top level 5 otime 2026-09-01 10:00:00 path @snapshots/root-20260901`), y `btrfs-progs` no escapa espacios dentro de `path`.
3. **`otime` se imprime en hora LOCAL del sistema** (`AAAA-MM-DD HH:MM:SS`), sin ninguna bandera para pedir epoch Unix -- a diferencia del `-p` de ZFS.

## Decisión

1. **Descubrimiento de montajes vía `/proc/mounts`, releído de forma independiente.** `btrfsMountpoints()` abre `/proc/mounts` y `parseBtrfsMountpoints` filtra por la columna de fstype (`btrfs`), quedándose con la columna de punto de montaje -- sin importar `internal/storage/diskinfo` (mismo criterio de paquetes hermanos independientes ya establecido entre `diskinfo`/`raidinfo`/`snapshotinfo`): aquí solo hacen falta 2 columnas de cada línea, no el resto del análisis de tamaños/exclusión de pseudo-filesystems que sí necesita `diskinfo`.
2. **`path` es siempre el ÚLTIMO campo con nombre.** `parseBtrfsSubvolumes` tokeniza cada línea por espacios y, al encontrar el token literal `path`, une de nuevo todo lo que queda como la ruta completa, sea cual sea su contenido -- única forma robusta de tolerar espacios embebidos sin escapar, dado que btrfs-progs no ofrece nada mejor.
3. **`otime` se parsea con `time.ParseInLocation("2006-01-02 15:04:05", ts, time.Local)`.** Una fecha no parseable dentro de esa línea deja `CreatedAt` en cero, nunca aborta la línea completa (`path` es el único campo indispensable).
4. **El ID numérico interno de btrfs (tras el primer token `ID`) NO se usa como `Snapshot.ID`.** No es un identificador globalmente significativo por sí solo (a diferencia del nombre completo `pool/dataset@snapshot` de un snapshot ZFS). En su lugar, `Snapshot.ID = mountpoint + ":" + path` y `VolumeName = mountpoint` -- el identificador que un administrador reconocería.
5. **`btrfs` ausente del `PATH`, o ningún montaje Btrfs encontrado, o un montaje concreto que falle (permisos) -> lista vacía, nunca `ErrUnsupported`.** Mismo criterio que ZFS ausente (ADR-021): Linux sí tiene adaptador, esta vía concreta no encuentra nada. Un montaje que falle no interrumpe a los demás.
6. **`enumerate` en Linux ahora agrega ZFS y Btrfs**, cada una degradando a vacío de forma completamente independiente -- ninguna de las dos propaga error hacia afuera, así que la ausencia de una nunca oculta lo que la otra sí encuentre. Esto reemplaza el cuerpo de Slice 8 (solo ZFS), exactamente el ajuste que su propio comentario ya dejaba anotado como pendiente.
7. **`Persistent` siempre `true`**, mismo criterio que ZFS: un subvolumen Btrfs es un objeto real en disco, no algo efímero como una instantánea VSS sin configuración de persistencia.
8. **Reutiliza el `Snapshot` público ya existente, sin ningún cambio de tipo** -- mismo criterio que ZFS y que `RaidArray` en `raidinfo`.

## Consecuencias

- `nexuscloud storage snapshots` ahora agrega VSS (Windows) o ZFS+Btrfs (Linux) sin ningún cambio de CLI.
- **Verificación limitada, aceptada explícitamente, más que ZFS -- causa raíz precisada el 2026-09-13 (Tarea #32)**: `parseBtrfsSubvolumes`/`parseBtrfsMountpoints` están probados con 7 tests unitarios contra texto sintético basado en el formato documentado de `btrfs-progs`, pero nunca contra una salida real de `btrfs subvolume list`. Se reintentó con un contenedor `--privileged` (ya autorizado por el usuario): `mkfs.btrfs` (puramente userspace) SÍ funcionó de verdad sobre un fichero loop, creando un filesystem Btrfs real en disco -- pero `btrfs subvolume list` exige que ese filesystem esté MONTADO (Decisión 1: opera sobre una ruta montada, no sobre una imagen cruda), y montar Btrfs exige el módulo de kernel `btrfs.ko`, que (igual hallazgo que ADR-018/021) no existe en ningún contenedor de este Docker Desktop -- `/lib/modules/<kernel>/` vacío, `/proc/filesystems` sin `btrfs` listado. Es decir: a diferencia de RAID/ZFS (donde nada pudo crearse en absoluto), aquí SÍ se creó un Btrfs real, pero la verificación en concreto que hace falta (`subvolume list` sobre un montaje real) sigue bloqueada por la misma causa raíz de fondo -- el kernel compartido de Docker Desktop en Windows. Si el formato de texto de `btrfs subvolume list` cambiara de forma incompatible en una versión futura de `btrfs-progs` (posible: es texto para humanos, no una API estable como `-H -p` en ZFS), el fallo se manifestaría como líneas ignoradas silenciosamente (nunca un panic, por diseño) en vez de un error visible -- riesgo aceptado y documentado, no descubierto en producción sin más. La única vía real para cerrar esta verificación es un Linux nativo con Btrfs soportado en el kernel (la inmensa mayoría de kernels Linux modernos lo traen).
- Lo que SÍ se verificó de verdad: la ausencia real de los binarios `zfs` y `btrfs` en un contenedor Linux real (`golang:1.25-bookworm`) se maneja con elegancia -- `nexuscloud storage snapshots` da "No se detectaron instantáneas." con exit 0, ejercitando de verdad ambas ramas `exec.LookPath` fallando y agregándose sin ocultarse entre sí.
- Con este slice, Snapshots alcanza la misma cobertura que RAID (Linux+Windows) y Fase 5 cierra sus tres pilares (Backup Manager, RAID, Snapshots) al nivel acordado con el usuario.

## Referencias

- [ADR-020: Detección de Snapshots en Windows vía VSS](ADR-020-snapshot-detection.md)
- [ADR-021: Detección de Snapshots en Linux vía ZFS](ADR-021-snapshot-zfs.md)
- [ADR-018: Detección de RAID en Linux vía mdadm](ADR-018-raid-detection.md) -- mismo criterio de "ausente = vacío, no ErrUnsupported" y de privilegios de bloque denegados por el sandbox.
