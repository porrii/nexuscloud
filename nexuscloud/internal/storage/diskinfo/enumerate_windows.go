//go:build windows

package diskinfo

import (
	"context"
	"sort"
	"strings"

	"golang.org/x/sys/windows"
)

// Valores estables de la Win32 API (winbase.h). Se definen aquí en vez de
// depender de que x/sys/windows los exporte con un nombre concreto.
const (
	driveRemovable       = 2
	driveFixed           = 3
	fileReadOnlyVolume   = 0x00080000
	volumeNameBufferSize = 261 // MAX_PATH + 1, en uint16
)

func enumerate(ctx context.Context) ([]Disk, error) {
	roots, err := logicalDriveRoots()
	if err != nil {
		return nil, err
	}

	var disks []Disk
	for _, root := range roots {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		rootPtr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}

		switch windows.GetDriveType(rootPtr) {
		case driveFixed, driveRemovable:
			// unidades de almacenamiento reales
		default:
			continue // red, CD-ROM, RAM disk, sin raíz: se omiten
		}

		var freeToCaller, total, totalFree uint64
		if err := windows.GetDiskFreeSpaceEx(rootPtr, &freeToCaller, &total, &totalFree); err != nil {
			continue
		}
		if total == 0 {
			continue
		}

		d := Disk{
			MountPoint: root,
			TotalBytes: total,
			FreeBytes:  freeToCaller,
			UsedBytes:  total - totalFree,
		}

		volName := make([]uint16, volumeNameBufferSize)
		fsName := make([]uint16, volumeNameBufferSize)
		var fsFlags, maxCompLen, serial uint32
		if err := windows.GetVolumeInformation(
			rootPtr,
			&volName[0], uint32(len(volName)),
			&serial, &maxCompLen, &fsFlags,
			&fsName[0], uint32(len(fsName)),
		); err == nil {
			d.Label = strings.TrimRight(windows.UTF16ToString(volName), "\x00")
			d.Filesystem = strings.TrimRight(windows.UTF16ToString(fsName), "\x00")
			d.ReadOnly = fsFlags&fileReadOnlyVolume != 0
		}

		disks = append(disks, d)
	}

	sort.Slice(disks, func(i, j int) bool { return disks[i].MountPoint < disks[j].MountPoint })
	return disks, nil
}

// logicalDriveRoots devuelve las raíces de unidad del sistema ("C:\\",
// "D:\\", ...) usando GetLogicalDriveStrings, que rellena un buffer con
// cadenas terminadas en NUL y un NUL final.
func logicalDriveRoots() ([]string, error) {
	n, err := windows.GetLogicalDriveStrings(0, nil)
	if err != nil {
		return nil, err
	}
	buf := make([]uint16, n)
	n, err = windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0])
	if err != nil {
		return nil, err
	}

	var roots []string
	start := 0
	for i := 0; i < int(n); i++ {
		if buf[i] == 0 {
			if i > start {
				roots = append(roots, windows.UTF16ToString(buf[start:i]))
			}
			start = i + 1
		}
	}
	return roots, nil
}
