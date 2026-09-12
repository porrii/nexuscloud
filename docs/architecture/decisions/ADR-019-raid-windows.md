# ADR-019: Detección de RAID en Windows vía PowerShell/Storage Spaces

## Estado

Aceptado.

## Contexto

ADR-018 (Slice 5) cubrió §12 solo en Linux (mdadm), dejando `ErrUnsupported` explícito en Windows -- Storage Spaces es el mecanismo que el propio §12 exige detectar ahí. A diferencia del caso Linux, la máquina de desarrollo de este slice es Windows real, lo que permitió investigar y verificar contra la API real en vez de solo contra documentación.

**Investigación con evidencia real de esta máquina**: `Get-StoragePool`/`Get-VirtualDisk`/`Get-PhysicalDisk` (módulo `Storage`, incluido en Windows 10 Pro/Education, no solo Server) están disponibles y devuelven datos reales. `Get-VirtualDisk` en esta máquina (sin ningún Virtual Disk configurado) devuelve una lista vacía real -- confirma el caso "sin arrays" contra Windows genuino. `ConvertTo-Json -InputObject @(...)` da array consistente incluso con 0 o 1 elementos en esta versión de PowerShell (Windows PowerShell 5.1; `-AsArray` no existe aquí, es de PowerShell 7+).

## Decisión

1. **PowerShell (`powershell.exe -Command`), no bindings Win32/COM nativos.** A diferencia de `internal/storage/diskinfo/enumerate_windows.go` (que usa syscalls Win32 directos -- `GetDriveType`, `GetDiskFreeSpaceEx`, etc. -- para enumeración básica de discos), Storage Spaces **no tiene una API Win32 clásica**: es WMI/CIM puro, y PowerShell es la interfaz que Microsoft documenta y mantiene para gestionarlo. Bindings COM/WMI nativos en Go serían más "puros" pero no tienen precedente en este proyecto y son notablemente más frágiles de acertar a la primera (inicialización COM, threading de apartamento, etc.) que invocar `powershell.exe` y parsear su JSON.
2. **`ConvertTo-Json -InputObject @(...)`, con el envoltorio `@()` obligatorio.** Sin él, un único resultado se serializa como objeto JSON suelto en vez de array de un elemento -- confirmado contra esta instalación real, no asumido de la documentación.
3. **Campos mapeados al mismo `RaidArray`/`RaidDevice` que ya usa el adaptador Linux** (sin cambios en `raidinfo.go`): `FriendlyName`→`Name`, `ResiliencySettingName` ("Simple"/"Mirror"/"Parity") → `Level` tal cual, sin forzarlo a nomenclatura "raidN" (son conceptos de Storage Spaces; forzarlos a RAID induciría a error a un administrador Windows). `Degraded` = `HealthStatus != "Healthy"` o `OperationalStatus != "OK"`. `Recovering` = `OperationalStatus` contiene "Repairing".
4. **`TotalDevices`/`ActiveDevices`/`Devices` quedan vacíos; `RecoveryPct` queda en 0, deliberadamente.** Obtener los discos físicos concretos de un Virtual Disk exige una segunda consulta de correlación (`Get-PhysicalDisk` vía el Storage Pool); el porcentaje de reconstrucción exigiría correlacionar con `Get-StorageJob`. Ambos quedan fuera de este slice -- §12 solo exige *mostrar el estado*, ya cubierto por `Degraded`/`Recovering`. Mismo criterio de no sobre-construir que ya limitó el adaptador Linux a lo que pide §12 (sin SMART, sin modelo/número de serie).
5. **Cualquier fallo degrada a `ErrUnsupported`** (powershell.exe no encontrado, módulo Storage no instalado, JSON inesperado) -- nunca panic, nunca un error confuso para el administrador.
6. **Sin crear un Storage Pool/Virtual Disk real en esta máquina para forzar el caso degradado.** Sería una operación difícil de revertir sobre el sistema real del usuario, desproporcionada para verificar un CLI de solo lectura -- mismo principio que ya evitó montar un array mdadm real con privilegios de bloque en el Slice 5.

## Consecuencias

- `nexuscloud storage raid` (sin ningún cambio de CLI: el mismo comando del Slice 5 ya delega en `raidinfo.Enumerate` sin ninguna rama por SO) ahora también funciona en Windows.
- **Verificación más fuerte que la del Slice 5**: el *mecanismo* completo (invocar `powershell.exe` de verdad, parsear su JSON de verdad) se verificó en vivo contra Windows real -- se cruzó (`GOOS=windows`) tanto el binario de tests como uno de prueba desde el contenedor Linux de `scripts/dev.sh` y se ejecutaron directamente sobre esta máquina, no solo en Docker/Linux. La detección *positiva* (Virtual Disk degradado) sigue verificada solo con JSON sintético en tests unitarios, igual que en Linux -- pero el primer capturado (lista vacía) es JSON real de esta máquina, no inventado.
- Añadir `Get-StorageJob` para el porcentaje de reconstrucción, o `Get-PhysicalDisk` correlacionado para la lista de discos por array, son extensiones futuras de bajo riesgo sobre este mismo diseño -- no exigen rehacer nada de lo ya construido.
