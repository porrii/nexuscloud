# ADR-020: Detección de Snapshots en Windows vía VSS (Volume Shadow Copy Service)

## Estado

Aceptado.

## Contexto

Con el Backup Manager (slices 1-4) y RAID (slices 5-6, Linux+Windows) maduros, el tercer pilar de Fase 5 -- Snapshots -- seguía sin ningún código. §17 exige: nunca implementar snapshots propios, **detectar** los que ya ofrezca el sistema de almacenamiento subyacente (ZFS, Btrfs, Storage Spaces) y documentar las capacidades según plataforma. En Windows, el mecanismo real detrás de "snapshots" a nivel de volumen NTFS es VSS (Volume Shadow Copy Service), no Storage Spaces directamente.

Mismo criterio que en RAID (ADR-018/019): se empieza por Windows porque esta máquina de desarrollo es Windows real, permitiendo investigar contra la API real en vez de solo contra documentación.

**Investigación con evidencia real de esta máquina**:
- `Get-CimInstance -ClassName Win32_ShadowCopy` funciona **sin privilegios de administrador** y devuelve `[]` real (sin ninguna instantánea existente ahora mismo).
- `vssadmin list shadowstorage` (espacio reservado para snapshots por volumen) **exige privilegios elevados** -- confirmado con el error real de esta máquina al ejecutarlo sin elevar.
- **Hallazgo real, no documental**: `ConvertTo-Json` serializa los campos `DateTime` de WMI en el formato heredado `"/Date(1621845782000)/"` (milisegundos desde epoch Unix), **no ISO-8601** -- confirmado contra `Win32_OperatingSystem.InstallDate` en esta misma máquina (`Win32_ShadowCopy` no tenía ninguna instancia real para probarlo directamente en el momento del diseño). De no haberse comprobado, un `time.Parse(time.RFC3339, ...)` en Go habría fallado en cuanto existiera un snapshot real.

## Decisión

1. **PowerShell (`Get-CimInstance Win32_ShadowCopy`), mismo patrón que Storage Spaces en `raidinfo` (ADR-019).** VSS es WMI/CIM, sin API Win32 clásica más simple.
2. **Nunca `vssadmin list shadowstorage` ni ningún comando que exija privilegios elevados.** Es información adicional (cuánto espacio hay reservado), no esencial para "detectar si hay snapshots" -- pedir elevación para un comando de solo lectura no está justificado.
3. **Parseo explícito del formato `/Date(ms)/`** (`parseWmiDate`, regex + `time.UnixMilli`), en vez de asumir un formato estándar -- ver el hallazgo real arriba. Un `InstallDate` en un formato no reconocido deja `CreatedAt` en su valor cero en vez de descartar el snapshot entero: un snapshot sin fecha legible sigue siendo información útil.
4. **`internal/storage/snapshotinfo` es un paquete nuevo, no una extensión de `raidinfo`.** Un snapshot y un array RAID son conceptos de almacenamiento relacionados pero independientes (mismo criterio que separó `raidinfo` de `diskinfo` en su momento).
5. **Linux (ZFS/Btrfs) queda `ErrUnsupported` explícitamente, para un slice futuro dedicado.** Mecanismo y verificación completamente distintos de VSS -- mismo criterio que dejó Storage Spaces pendiente tras el slice 5 de RAID hasta tener evidencia real contra la que diseñar.
6. **Nunca crear ni eliminar snapshots.** §17 es sobre integrarse con los que ya existen, nunca gestionarlos activamente -- gestionarlos sería acercarse a "implementar snapshots propios", justo lo que el requisito prohíbe. Tampoco se crea una instantánea VSS real en esta máquina para verificar el caso "con datos": `vssadmin create shadow` también exige privilegios elevados y modificaría el sistema real del usuario -- mismo principio que ya evitó esto en RAID (ADR-018/019).

## Consecuencias

- `nexuscloud storage snapshots` da una primera respuesta a §17 en Windows, sin ningún riesgo (solo lectura, sin privilegios elevados).
- Linux sigue sin ninguna detección de snapshots -- una instancia NexusCloud en un servidor con ZFS/Btrfs no verá nada todavía; documentado como hueco conocido, no silenciado.
- El hallazgo del formato `/Date(ms)/` es reutilizable: cualquier futuro adaptador Windows que consuma `ConvertTo-Json` sobre una clase WMI con campos `DateTime` (p.ej. una extensión futura de `raidinfo` para `Get-StorageJob`) debe tenerlo en cuenta desde el principio.
- Verificado con el mismo rigor que el slice 6 de RAID: binario de tests y binario completo de `nexuscloud` cruzados a Windows (`GOOS=windows`) desde el contenedor Linux y ejecutados nativamente sobre esta máquina real -- confirma en vivo el mecanismo completo (subproceso + PowerShell + JSON reales) para la ruta "sin instantáneas", el único estado que este entorno permite probar sin modificar el sistema real del usuario.
