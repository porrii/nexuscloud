// Package raidinfo detecta, en modo solo lectura, arrays RAID gestionados
// por el sistema operativo (§12: "NO implementes un sistema RAID propietario
// dentro de NexusCloud... detecta y utiliza tecnologías proporcionadas por
// el sistema operativo o hardware").
//
// Hay un adaptador por sistema operativo (build tags), mismo patrón que
// internal/storage/diskinfo: si la plataforma actual no tiene adaptador,
// Enumerate devuelve ErrUnsupported y el resto del sistema sigue
// funcionando con normalidad -- nunca depende de una única tecnología.
//
// Adaptadores implementados: Linux (mdadm software RAID, vía
// /proc/mdstat) y Windows (Storage Spaces, vía PowerShell/Get-VirtualDisk
// -- WMI/CIM puro, sin API Win32 clásica, a diferencia de la enumeración
// básica de discos). En el resto de plataformas, Enumerate devuelve
// ErrUnsupported. RAID hardware (controladoras dedicadas) y JBOD quedan
// fuera de cualquier adaptador: no hay una fuente de datos portable sin
// herramientas privilegiadas adicionales.
package raidinfo

import (
	"context"
	"errors"
)

// ErrUnsupported se devuelve cuando la plataforma actual no tiene un
// adaptador de detección de RAID. No es un fallo del sistema: quien llame
// debe degradar limpiamente (p.ej. no mostrar la sección de RAID).
var ErrUnsupported = errors.New("raidinfo: detección de RAID no soportada en esta plataforma")

// RaidArray describe un array RAID tal como lo ve el sistema operativo.
type RaidArray struct {
	Name          string // "md0"
	Level         string // "raid0", "raid1", "raid5", "raid6", "raid10", ...
	Active        bool   // false si el array está "inactive"
	TotalDevices  int    // el N de "[N/M]": dispositivos configurados
	ActiveDevices int    // el M de "[N/M]": dispositivos actualmente activos
	// Degraded es true si ActiveDevices < TotalDevices, o si el patrón de
	// estado por dispositivo (p.ej. "[UU]") trae algún hueco ('_') -- la
	// señal de "riesgo de pérdida de datos" que exige §12.
	Degraded bool
	// Recovering es true mientras el array está reconstruyéndose
	// (resync/recovery en curso) -- §12 "rebuild".
	Recovering bool
	// RecoveryPct es el progreso (0-100) cuando Recovering es true; 0 en
	// caso contrario.
	RecoveryPct float64
	Devices     []RaidDevice
}

// RaidDevice es un miembro de un RaidArray.
type RaidDevice struct {
	Name   string // "sda1"
	Role   int    // el número entre corchetes, p.ej. sda1[0] -> 0
	Faulty bool   // sufijo (F): dispositivo fallido
	Spare  bool   // sufijo (S): dispositivo de repuesto, no forma parte activa del array
}

// Enumerate devuelve los arrays RAID detectados. Una lista vacía (sin
// error) significa "el sistema operativo soporta esto, pero no hay ningún
// array configurado" -- distinto de ErrUnsupported, que significa "esta
// plataforma no tiene ningún adaptador todavía".
func Enumerate(ctx context.Context) ([]RaidArray, error) {
	return enumerate(ctx)
}
