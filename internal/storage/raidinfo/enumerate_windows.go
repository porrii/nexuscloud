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
	const script = `ConvertTo-Json -InputObject @(Get-VirtualDisk -ErrorAction SilentlyContinue | ` +
		`Select-Object FriendlyName,ResiliencySettingName,OperationalStatus,HealthStatus)`

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

// virtualDisk es la forma mínima que se le pide a PowerShell -- ver
// Select-Object en el script de arriba. Cualquier otro campo de
// MSFT_VirtualDisk se ignora a propósito.
type virtualDisk struct {
	FriendlyName          string `json:"FriendlyName"`
	ResiliencySettingName string `json:"ResiliencySettingName"`
	OperationalStatus     string `json:"OperationalStatus"`
	HealthStatus          string `json:"HealthStatus"`
}

// parseVirtualDisks decodifica la salida de Get-VirtualDisk (ver
// enumerate) y la mapea al mismo RaidArray que usa el adaptador Linux.
// Separada de la invocación real a powershell.exe para poder probarla con
// JSON sintético, mismo criterio que parseMdstat.
//
// TotalDevices/ActiveDevices/Devices quedan vacíos deliberadamente: qué
// discos físicos concretos forman un Virtual Disk exige una segunda
// consulta de correlación (Get-PhysicalDisk vía el Storage Pool) fuera de
// alcance de este slice -- §12 solo exige mostrar el estado, ya cubierto
// por Degraded/Recovering. RecoveryPct queda en 0 por el mismo motivo
// (exigiría correlacionar con Get-StorageJob).
func parseVirtualDisks(data []byte) ([]RaidArray, error) {
	var disks []virtualDisk
	if err := json.Unmarshal(data, &disks); err != nil {
		return nil, err
	}
	arrays := make([]RaidArray, 0, len(disks))
	for _, d := range disks {
		arrays = append(arrays, RaidArray{
			Name:       d.FriendlyName,
			Level:      d.ResiliencySettingName,
			Active:     true,
			Degraded:   d.HealthStatus != "Healthy" || d.OperationalStatus != "OK",
			Recovering: strings.Contains(d.OperationalStatus, "Repairing"),
		})
	}
	return arrays, nil
}
