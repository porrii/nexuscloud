//go:build windows

package raidinfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
)

// enumerate en Windows consulta Storage Spaces vía PowerShell -- a
// diferencia de internal/storage/diskinfo/enumerate_windows.go (que usa
// syscalls Win32 directos para enumeración básica de discos), Storage
// Spaces no tiene una API Win32 clásica: es WMI/CIM puro, y PowerShell es
// la interfaz que Microsoft documenta y mantiene para gestionarlo. Añadir
// bindings COM/WMI nativos en Go sería más "puro" pero sin precedente en
// este proyecto y notablemente más frágil de acertar a la primera.
//
// Cualquier fallo (powershell.exe no encontrado, módulo Storage no
// instalado, JSON inesperado) degrada a ErrUnsupported -- nunca panic, ni
// un error confuso para el administrador.
func enumerate(ctx context.Context) ([]RaidArray, error) {
	// -ErrorAction SilentlyContinue: si el módulo Storage no está instalado
	// o no hay ningún Virtual Disk, Get-VirtualDisk no debe hacer fallar
	// todo el pipeline. El envoltorio @(...) fuerza salida en array de
	// ConvertTo-Json incluso con 0 o 1 elementos (confirmado contra una
	// instalación real: sin él, un único resultado se serializa como
	// objeto suelto, no como array de un elemento).
	//
	// Get-PhysicalDisk -VirtualDisk / Get-StorageJob -VirtualDisk (ambos
	// confirmados como parameter sets reales de esos cmdlets, ADR-027) se
	// correlacionan DENTRO del propio script -- un solo round-trip a
	// powershell.exe en vez de uno por cada Virtual Disk encontrado.
	//
	// -Depth 4 es imprescindible: confirmado con un objeto sintético en una
	// máquina Windows real que, sin -Depth, ConvertTo-Json trunca cualquier
	// nivel de anidación por encima del segundo a una cadena de texto tipo
	// "@{FriendlyName=...}" en vez de JSON real (ADR-027) -- con
	// PhysicalDisks/Jobs anidados dentro de cada Virtual Disk, hacen falta
	// 4 niveles (array de Virtual Disks -> objeto -> array anidado ->
	// objeto anidado).
	const script = `$result = foreach ($vd in (Get-VirtualDisk -ErrorAction SilentlyContinue)) {` +
		`[PSCustomObject]@{` +
		`FriendlyName = $vd.FriendlyName; ResiliencySettingName = $vd.ResiliencySettingName; ` +
		`OperationalStatus = $vd.OperationalStatus; HealthStatus = $vd.HealthStatus; ` +
		`PhysicalDisks = @(Get-PhysicalDisk -VirtualDisk $vd -ErrorAction SilentlyContinue | Select-Object FriendlyName,Usage,HealthStatus); ` +
		`Jobs = @(Get-StorageJob -VirtualDisk $vd -ErrorAction SilentlyContinue | Select-Object PercentComplete)}}; ` +
		`ConvertTo-Json -InputObject @($result) -Depth 4`

	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, ErrUnsupported
	}
	arrays, err := parseVirtualDisks(out)
	if err != nil {
		return nil, ErrUnsupported
	}
	return arrays, nil
}

// physicalDiskInfo es la forma mínima que se le pide a Get-PhysicalDisk --
// ver Select-Object en el script de enumerate.
type physicalDiskInfo struct {
	FriendlyName string `json:"FriendlyName"`
	Usage        string `json:"Usage"`
	HealthStatus string `json:"HealthStatus"`
}

// storageJobInfo es la forma mínima que se le pide a Get-StorageJob.
type storageJobInfo struct {
	PercentComplete float64 `json:"PercentComplete"`
}

// virtualDisk es la forma mínima que se le pide a PowerShell -- ver
// Select-Object en el script de arriba. Cualquier otro campo de
// MSFT_VirtualDisk se ignora a propósito. PhysicalDisks/Jobs pueden venir
// como JSON null (no solo array vacío) si esa parte de la correlación no
// encontró nada -- Unmarshal deja el slice de Go en nil en ese caso, que
// se recorre igual que un slice vacío, sin necesitar ningún caso especial.
type virtualDisk struct {
	FriendlyName          string             `json:"FriendlyName"`
	ResiliencySettingName string             `json:"ResiliencySettingName"`
	OperationalStatus     string             `json:"OperationalStatus"`
	HealthStatus          string             `json:"HealthStatus"`
	PhysicalDisks         []physicalDiskInfo `json:"PhysicalDisks"`
	Jobs                  []storageJobInfo   `json:"Jobs"`
}

// parseVirtualDisks decodifica la salida de Get-VirtualDisk (ver
// enumerate) y la mapea al mismo RaidArray que usa el adaptador Linux.
// Separada de la invocación real a powershell.exe para poder probarla con
// JSON sintético, mismo criterio que parseMdstat.
func parseVirtualDisks(data []byte) ([]RaidArray, error) {
	var disks []virtualDisk
	if err := json.Unmarshal(data, &disks); err != nil {
		return nil, err
	}
	arrays := make([]RaidArray, 0, len(disks))
	for _, d := range disks {
		devices := make([]RaidDevice, 0, len(d.PhysicalDisks))
		activeCount := 0
		for _, pd := range d.PhysicalDisks {
			faulty := pd.HealthStatus != "Healthy"
			if !faulty {
				activeCount++
			}
			devices = append(devices, RaidDevice{
				// Role queda en su cero: mdadm tiene un índice de posición
				// dentro del array (sda1[0]) sin equivalente directo en
				// Storage Spaces -- no se inventa un número sin sentido.
				Name:   pd.FriendlyName,
				Faulty: faulty,
				Spare:  pd.Usage == "Hot Spare",
			})
		}

		recovering := strings.Contains(d.OperationalStatus, "Repairing")
		var recoveryPct float64
		if recovering && len(d.Jobs) > 0 {
			// Un MSFT_StorageJob puede representar otras operaciones
			// además de una reparación (ADR-027) -- se condiciona a la
			// señal Recovering ya derivada arriba, nunca a la mera
			// presencia de un job, para no mostrar un progreso que no
			// corresponda a una reconstrucción real.
			recoveryPct = d.Jobs[0].PercentComplete
		}

		arrays = append(arrays, RaidArray{
			Name:          d.FriendlyName,
			Level:         d.ResiliencySettingName,
			Active:        true,
			Degraded:      d.HealthStatus != "Healthy" || d.OperationalStatus != "OK",
			Recovering:    recovering,
			RecoveryPct:   recoveryPct,
			TotalDevices:  len(devices),
			ActiveDevices: activeCount,
			Devices:       devices,
		})
	}
	return arrays, nil
}
