// Package diskinfo enumera, en modo solo lectura, las unidades de
// almacenamiento que ve el sistema operativo (§11 / requisito 3 de la
// actualización 2026-09-10: "detectar automáticamente todas las unidades
// disponibles" con capacidad, espacio libre/usado, sistema de archivos,
// punto de montaje y estado).
//
// Hay un adaptador por sistema operativo (build tags): Linux vía
// /proc/mounts + statfs, Windows vía las APIs de volumen de la Win32 API,
// y un stub para el resto. El núcleo de NexusCloud nunca depende de una
// única API del sistema: si la plataforma actual no tiene adaptador,
// Enumerate devuelve ErrUnsupported y el resto del sistema sigue
// funcionando con normalidad.
//
// Datos avanzados (modelo, número de serie, temperatura, SMART, tipo de
// disco SSD/HDD, estado RAID) quedan deliberadamente fuera de esta primera
// versión: requieren utilidades externas (smartctl) o ioctls privilegiados
// y el propio spec los condiciona a "cuando sea seguro y apropiado".
package diskinfo

import (
	"context"
	"errors"
)

// ErrUnsupported se devuelve cuando la plataforma actual no tiene un
// adaptador de enumeración de discos. No es un fallo del sistema: quien
// llame debe degradar limpiamente (p.ej. no mostrar la sección de discos).
var ErrUnsupported = errors.New("diskinfo: enumeración de discos no soportada en esta plataforma")

// Disk describe una unidad de almacenamiento tal como la ve el sistema
// operativo. Los campos opcionales (Device, Label) pueden venir vacíos si
// el SO no los expone de forma portable y segura.
type Disk struct {
	MountPoint string // punto de montaje / raíz de la unidad ("/", "/mnt/datos", "C:\\")
	Filesystem string // "ext4", "xfs", "btrfs", "ntfs", "apfs", ...
	Device     string // "/dev/sda1", "\\\\?\\Volume{...}", ... (opcional)
	Label      string // etiqueta de volumen (opcional)

	TotalBytes uint64
	FreeBytes  uint64 // espacio disponible para escritura sin privilegios
	UsedBytes  uint64 // TotalBytes - (espacio libre real)

	ReadOnly bool // montada en solo lectura
}

// Enumerate devuelve las unidades de almacenamiento "reales" del sistema
// (se omiten los pseudo-sistemas de archivos: proc, sysfs, cgroup, tmpfs
// del sistema, overlay de contenedores, etc.). El orden es estable por
// punto de montaje. Devuelve ErrUnsupported si no hay adaptador para la
// plataforma actual.
func Enumerate(ctx context.Context) ([]Disk, error) {
	return enumerate(ctx)
}
