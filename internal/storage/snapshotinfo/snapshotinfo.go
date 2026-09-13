// Package snapshotinfo detecta, en modo solo lectura, snapshots ya
// existentes en el sistema de almacenamiento subyacente (§17: "Integra
// snapshots siempre que el sistema de almacenamiento subyacente lo
// permita. No implementes un sistema de snapshots propietario").
//
// Mismo patrón que internal/storage/diskinfo y raidinfo: un adaptador por
// sistema operativo vía build tags; sin adaptador para la plataforma
// actual, Enumerate devuelve ErrUnsupported en vez de romper la
// compilación o fingir soporte que no existe.
//
// Adaptadores implementados: Windows (VSS/Volume Shadow Copy Service, vía
// PowerShell/Get-CimInstance Win32_ShadowCopy) y Linux, que agrega dos vías
// independientes -- ZFS (vía el binario "zfs") y Btrfs (subvolúmenes de solo
// lectura, vía "btrfs subvolume list" sobre cada montaje Btrfs encontrado en
// /proc/mounts). En Linux, cada vía ausente (binario no instalado, o sin
// nada que reportar) degrada a lista vacía sin afectar a la otra -- nunca a
// ErrUnsupported, que queda reservado solo para plataformas sin ningún
// adaptador en absoluto. NexusCloud nunca crea ni elimina snapshots: §17 es
// sobre integrarse con los que ya existen, nunca gestionarlos activamente.
package snapshotinfo

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported se devuelve cuando la plataforma actual no tiene un
// adaptador de detección de snapshots. No es un fallo del sistema: quien
// llame debe degradar limpiamente (p.ej. no mostrar la sección).
var ErrUnsupported = errors.New("snapshotinfo: detección de snapshots no soportada en esta plataforma")

// Snapshot describe una instantánea ya existente, tal como la ve el
// sistema operativo.
type Snapshot struct {
	ID         string
	VolumeName string // p.ej. "\\?\Volume{GUID}\" en Windows
	// CreatedAt queda en su valor cero si el sistema operativo no expone
	// una fecha en un formato reconocido -- un snapshot sin fecha legible
	// sigue siendo información útil, no se descarta por eso.
	CreatedAt  time.Time
	Persistent bool // sobrevive a un reinicio
}

// Enumerate devuelve las instantáneas detectadas. Una lista vacía (sin
// error) significa "el sistema operativo soporta esto, pero no hay
// ninguna instantánea ahora mismo" -- distinto de ErrUnsupported, que
// significa "esta plataforma no tiene ningún adaptador todavía".
func Enumerate(ctx context.Context) ([]Snapshot, error) {
	return enumerate(ctx)
}
