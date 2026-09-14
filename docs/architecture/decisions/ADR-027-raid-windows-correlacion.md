# ADR-027: RAID Windows -- Correlación de Discos Físicos y Progreso de Reconstrucción (§12)

## Estado

Aceptado.

## Contexto

ADR-019 dejó `TotalDevices`/`ActiveDevices`/`Devices`/`RecoveryPct` de `RaidArray` sin rellenar en el adaptador Windows: correlacionar un Virtual Disk con sus discos físicos y con el progreso de una reconstrucción exige una segunda consulta, marcada explícitamente como extensión futura de bajo riesgo. Este slice la cierra.

## Investigación (evidencia real contra esta máquina Windows)

- `(Get-Command Get-PhysicalDisk).ParameterSets` y `Get-StorageJob` confirman un parameter set `ByVirtualDisk` en ambos cmdlets: `Get-PhysicalDisk -VirtualDisk <vd>` y `Get-StorageJob -VirtualDisk <vd>` son la vía de correlación real, verificada contra la definición real del cmdlet en esta máquina, no adivinada de la documentación.
- Propiedades confirmadas contra la clase CIM y contra los 3 discos físicos reales de esta máquina: `PhysicalDisk.FriendlyName/Usage/HealthStatus` salen como texto legible ("Auto-Select"/"Healthy"), igual que el resto de propiedades ya usadas en ADR-019 -- ninguna traducción de enum numérico que hacer en Go.
- **Hallazgo real nuevo**: `ConvertTo-Json` sin `-Depth` trunca cualquier objeto anidado a más de 2 niveles a una cadena de texto (`"@{Name=disk1; Usage=Auto-Select}"`) en vez de JSON real -- confirmado con un objeto sintético en esta máquina. Con `-Depth 4` serializa correctamente. Sin este hallazgo, el parser habría recibido basura silenciosa (fallo de `json.Unmarshal`, degradando a `ErrUnsupported`) en vez de datos reales, sin ningún mensaje que apuntara a la causa.
- Sin ningún Virtual Disk real en esta máquina (mismo estado que ADR-019), se confirmó que el bucle de correlación no lanza ninguna excepción con cero iteraciones.

## Decisión

1. **Un único script PowerShell combinado**, no una consulta de correlación por cada Virtual Disk desde Go: el propio script construye la estructura anidada (`PhysicalDisks`/`Jobs` embebidos en cada Virtual Disk) en un solo round-trip a `powershell.exe`.
2. **`RecoveryPct` condicionado a `Recovering` (ya derivado de `OperationalStatus` conteniendo "Repairing") Y a que exista al menos un job correlacionado** -- nunca a la mera presencia de un job. Un `MSFT_StorageJob` puede representar otras operaciones además de una reparación; sin un array real reconstruyéndose en ningún entorno disponible para contrastar el filtrado exacto por `JobState`, esta es la lectura más conservadora: nunca muestra un progreso que no corresponda a una reconstrucción real ya señalada por otra vía.
3. **`Role` de `RaidDevice` queda en su valor cero para los discos Windows.** mdadm tiene un índice de posición dentro del array (`sda1[0]`) sin equivalente directo en Storage Spaces -- no se inventa un número sin sentido solo por rellenar el campo.
4. **`TotalDevices`/`ActiveDevices` se derivan de los propios discos correlacionados** (`len(Devices)` / discos con `HealthStatus == "Healthy"`), sin una tercera consulta.
5. **CLI sin cambios**: `internal/cli/storage_cmd.go` ya imprimía `Devices`/`RecoveryPct` genéricamente desde que el adaptador Linux los rellenó (Fase 5 slice 5) -- este slice solo completa los mismos datos en el lado Windows.

## Consecuencias

- 5 tests nuevos en `enumerate_windows_test.go` (correlación positiva de `RecoveryPct`, `Recovering=false` pese a existir un job, discos físicos correlacionados con uno `Faulty`, uno `Spare`, y JSON con `PhysicalDisks`/`Jobs` en `null` sin panic) + 1 test existente actualizado.
- **Verificado con el mismo rigor que ADR-019/020**: los 9 tests de `enumerate_windows_test.go` se ejecutaron de verdad en un binario de tests Windows nativo cruzado desde el contenedor Linux y ejecutado en esta máquina real. `nexuscloud.exe storage raid` (binario completo, mismo cruce) confirmó "No se detectó ningún array RAID." con exit 0 -- regresión del camino ya E2E-verificado en ADR-019, con el bucle de correlación nuevo ejecutándose de verdad con cero iteraciones. `storage disks` (adaptador no relacionado) también se re-verificó sin problema.
- El parseo positivo de `PhysicalDisks`/`Jobs` (con datos reales de un array reconstruyéndose) sigue sin verificación contra un sistema real, mismo motivo que ADR-019: crear un Storage Pool/Virtual Disk real en esta máquina modificaría su almacenamiento real, desproporcionado para un CLI de solo lectura.
