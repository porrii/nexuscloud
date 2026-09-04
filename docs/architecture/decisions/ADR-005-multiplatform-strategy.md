# ADR-005: Estrategia multiplataforma — Capability Detection, no abstracciones que fingen soporte universal

## Estado

Aceptado (parcialmente implementado; ver "Estado real" más abajo).

## Contexto

§101 exige que NexusCloud nunca finja soportar una funcionalidad que la plataforma subyacente no ofrece realmente — mostrar "esta funcionalidad no está disponible aquí" en vez de simular soporte. Esto afecta sobre todo a Fase 5 (detección de discos/RAID/SMART, muy dependiente del SO), pero el patrón se decide ahora porque condiciona el diseño de `internal/storage` y de futuros paquetes `internal/disk`.

También cubre la estrategia de clientes: §6 pide reutilizar la mayor cantidad de código posible entre Windows, Linux, Android y Web.

## Decisión

1. **Capability Detection sobre abstracciones universales**: en vez de una interfaz que promete "listar discos" en todas las plataformas y falla en silencio donde no puede, cada adaptador específico de SO se registra explícitamente, y el código que lo consume comprueba disponibilidad y responde con un estado claro ("no disponible en esta plataforma") en vez de datos falsos o vacíos indistinguibles de "no hay discos".
2. Para el backend, esto se traduce en invocar herramientas nativas del SO vía `os/exec` cuando haga falta (`smartctl`, PowerShell `Get-PhysicalDisk`, `lsblk`) en vez de bindings CGO complejos — mantiene `CGO_ENABLED=0` (ver [ADR-003](ADR-003-database.md)) y degrada con gracia si la herramienta no está instalada.
3. Para los clientes (Fase 3-4, Flutter/Dart), se reutiliza explícitamente el patrón ya validado en NexusKeys (vault de contraseñas del mismo ecosistema, único precedente Flutter real disponible): Clean Architecture por feature (`domain`/`data`/`presentation` bajo `lib/features/`), `get_it` para inyección de dependencias, `flutter_riverpod` para estado, `flutter_secure_storage`/`local_auth` para credenciales y biometría. Reutilizar un patrón ya probado en el mismo ecosistema reduce el riesgo de reinventar decisiones ya validadas.

## Estado real en esta pasada

La Fase 1 **no implementa todavía** ningún adaptador de disco/RAID (eso es Fase 5) ni ningún cliente Flutter (Fase 3-4): este ADR documenta la decisión de diseño para cuando lleguen, de modo que no haya que rediseñar la arquitectura de `internal/storage` en ese momento. Lo que sí existe hoy que sigue este mismo espíritu es `internal/config`: cada plataforma tiene su propio `defaultDataDir()` (Windows: `%ProgramData%\NexusCloud`; resto: `/var/lib/nexuscloud`), sin asumir un único layout universal.

## Consecuencias

- Añadir Fase 5 (discos/RAID) no debería requerir cambiar el contrato de `storage.Provider` ni de `FileService`: los adaptadores de disco son un módulo de solo-lectura/monitorización paralelo, no parte de la ruta crítica de subida/descarga.
- El cliente Flutter reutilizará convenciones ya en producción en NexusKeys, reduciendo la superficie de decisiones nuevas cuando llegue la Fase 3.
- Riesgo aceptado: invocar binarios externos vía `os/exec` para capacidades de Fase 5 añade una dependencia de que esas herramientas estén instaladas en el sistema — mitigado por degradar explícitamente a "no disponible" en vez de fallar de forma opaca (§101).
