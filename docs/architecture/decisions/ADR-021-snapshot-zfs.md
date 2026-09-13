# ADR-021: Detección de Snapshots en Linux vía ZFS (`zfs list`)

## Estado

Aceptado.

## Contexto

El Slice 7 cubrió §17 en Windows (VSS). Este slice completa el pilar Snapshots en Linux, empezando por ZFS -- Btrfs queda para un slice futuro dedicado (modelo y formato de salida completamente distintos: un snapshot Btrfs es un subvolumen con la propiedad de solo lectura, no un tipo de objeto propio como en ZFS).

**Diferencia honesta frente a los slices anteriores de esta serie (RAID Linux/Windows, Snapshots Windows)**: todos ellos se apoyaron en evidencia empírica real -- `/proc/mdstat` es un fichero de formato de kernel bien conocido y directamente inspeccionable; Storage Spaces y VSS se investigaron contra la API real de una máquina Windows real. Aquí no hay ningún sistema con ZFS accesible durante el desarrollo (ni el contenedor Docker de la sesión -- confirmado con `which zfs` -- ni la máquina de desarrollo, que es Windows). Este slice se apoya en el formato de `zfs list` documentado y estable desde hace muchos años, no en evidencia propia. Es, con diferencia, el slice con menos verificación directa de toda esta serie de detección de almacenamiento, y se documenta así sin disimularlo.

## Decisión

1. **`zfs list -H -p -t snapshot -o name,creation`, no un pseudo-fichero.** A diferencia de mdadm (que expone su estado en `/proc/mdstat`, un fichero de texto plano del kernel), el estado de ZFS vive detrás de una ioctl privada del módulo de kernel: no hay equivalente a `/proc/mdstat` para snapshots ZFS, así que hace falta invocar el propio binario `zfs` -- mismo tipo de dependencia que ya tienen los adaptadores Windows de este proyecto (PowerShell).
2. **Banderas pensadas para scripting, nunca el formato humano.** `-H` (sin cabecera, campos separados por TAB) y `-p` (valores "parseable": `creation` sale como epoch Unix entero, no como fecha humana) eliminan la ambigüedad de columnas alineadas de ancho variable y de formatos de fecha regionales. Aplica directamente la lección del hallazgo real del Slice 7 (`ConvertTo-Json` serializando fechas WMI como `/Date(ms)/`): usar siempre la forma más parseable que el propio comando ofrezca, nunca la pensada para que la lea un humano.
3. **`zfs` ausente del `PATH` -> lista vacía (`nil, nil`), no `ErrUnsupported`.** Semánticamente distinto de "esta plataforma no tiene ningún adaptador" (`enumerate_other.go`): Linux SÍ tiene un adaptador real (este); simplemente esta vía concreta no encuentra nada que reportar en este sistema. Esto importa para cuando se añada Btrfs en un slice futuro: en ese momento, Linux debe intentar ambas vías antes de decidir que no hay nada, y `ErrUnsupported` seguiría reservado solo para plataformas sin ningún adaptador en absoluto.
4. **`zfs list` sin ningún pool importado (código de salida != 0) también se trata como lista vacía**, mismo criterio que `/proc/mdstat` ausente en `raidinfo` (ADR-018): un sistema real sin pools ZFS no es un fallo de NexusCloud.
5. **`Persistent` siempre `true` para un snapshot ZFS**, sin ninguna consulta adicional -- un snapshot ZFS es por naturaleza un objeto persistente en disco, a diferencia de una instantánea VSS que puede o no sobrevivir a un reinicio según su configuración.
6. **Reutiliza el `Snapshot` público ya existente (de VSS), sin ningún cambio de tipo.** `ID` = nombre completo (`pool/dataset@snapshot`); `VolumeName` = la parte antes de `@`. Mismo criterio que `RaidArray` compartido entre los adaptadores Linux y Windows de `raidinfo`.

## Consecuencias

- `nexuscloud storage snapshots` ahora también funciona en Linux, sin ningún cambio de CLI (mismo comando del Slice 7).
- **Verificación limitada, aceptada explícitamente**: el parseo de `zfs list` está probado con 5 tests unitarios contra texto sintético basado en el formato documentado, pero nunca contra una salida real de `zfs` -- instalar ZFS en el entorno de desarrollo para probarlo exigiría cargar un módulo de kernel con privilegios que el sandbox de esta sesión ya deniega para operaciones de bloque equivalentes (mdadm, ADR-018). Si el formato de `zfs list -H -p` cambiara de forma incompatible en alguna versión futura de ZFS (poco probable: son banderas estables desde hace más de una década), el fallo se manifestaría como líneas ignoradas silenciosamente (nunca un panic, por diseño) en vez de un error visible -- riesgo aceptado y documentado, no descubierto en producción sin más.
- Lo que SÍ se verificó de verdad: la ausencia real del binario `zfs` en un contenedor Linux real (`golang:1.25-bookworm`, sin ZFS instalado) se maneja con elegancia -- `nexuscloud storage snapshots` da "No se detectaron instantáneas." con exit 0, ejercitando de verdad la rama `exec.LookPath` que falla, no solo en teoría.
- Añadir Btrfs en un slice futuro exigirá revisar la rama "ZFS ausente -> vacío" de este slice para que intente ambas vías antes de concluir que no hay nada -- ya anotado en el propio código.
