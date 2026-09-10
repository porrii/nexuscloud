//go:build linux

package diskinfo

import (
	"bufio"
	"context"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// pseudoFilesystems son tipos de sistema de archivos que no representan una
// unidad de almacenamiento real (memoria, kernel, contenedores, imágenes
// de solo lectura de paquetes, etc.). Es el mismo criterio que usa `df`
// para no listarlos por defecto.
var pseudoFilesystems = map[string]bool{
	"autofs": true, "binfmt_misc": true, "bpf": true, "cgroup": true,
	"cgroup2": true, "configfs": true, "debugfs": true, "devpts": true,
	"devtmpfs": true, "efivarfs": true, "fuse.gvfsd-fuse": true, "fusectl": true,
	"hugetlbfs": true, "mqueue": true, "nsfs": true, "overlay": true,
	"proc": true, "pstore": true, "ramfs": true, "rpc_pipefs": true,
	"securityfs": true, "selinuxfs": true, "squashfs": true, "sysfs": true,
	"tmpfs": true, "tracefs": true,
}

type mountEntry struct {
	device     string
	mountPoint string
	fsType     string
	readOnly   bool
}

func enumerate(ctx context.Context) ([]Disk, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries, err := parseMountLines(f)
	if err != nil {
		return nil, err
	}

	var disks []Disk
	for _, e := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Solo unidades de verdad: un bind mount de un fichero suelto
		// (/etc/hosts, /etc/resolv.conf en contenedores; ficheros
		// individuales en algunos hosts) es una entrada de /proc/mounts
		// pero no una "unidad de almacenamiento".
		if fi, err := os.Stat(e.mountPoint); err != nil || !fi.IsDir() {
			continue
		}
		var st unix.Statfs_t
		if err := unix.Statfs(e.mountPoint, &st); err != nil {
			continue // punto de montaje inaccesible ahora mismo: se omite
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		if total == 0 {
			continue // sin capacidad reportable
		}
		disks = append(disks, Disk{
			MountPoint: e.mountPoint,
			Filesystem: e.fsType,
			Device:     e.device,
			TotalBytes: total,
			FreeBytes:  st.Bavail * bsize,
			UsedBytes:  (st.Blocks - st.Bfree) * bsize,
			ReadOnly:   e.readOnly,
		})
	}

	sort.Slice(disks, func(i, j int) bool { return disks[i].MountPoint < disks[j].MountPoint })
	return disks, nil
}

// parseMountLines interpreta el formato de /proc/mounts, descarta los
// pseudo-sistemas de archivos y deduplica por punto de montaje (bind
// mounts / entradas repetidas: la primera gana). Está separado de los
// syscalls para poder probarlo con entrada sintética.
func parseMountLines(r io.Reader) ([]mountEntry, error) {
	seen := make(map[string]bool)
	var out []mountEntry

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		fsType := fields[2]
		if pseudoFilesystems[fsType] {
			continue
		}
		mountPoint := unescapeMountField(fields[1])
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true

		out = append(out, mountEntry{
			device:     unescapeMountField(fields[0]),
			mountPoint: mountPoint,
			fsType:     fsType,
			readOnly:   hasOption(fields[3], "ro"),
		})
	}
	return out, sc.Err()
}

// unescapeMountField deshace los escapes octales de /proc/mounts (\040
// espacio, \011 tab, \012 salto de línea, \134 backslash).
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if c, ok := octalByte(s[i+1], s[i+2], s[i+3]); ok {
				b.WriteByte(c)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func octalByte(a, b, c byte) (byte, bool) {
	if a < '0' || a > '7' || b < '0' || b > '7' || c < '0' || c > '7' {
		return 0, false
	}
	return (a-'0')<<6 | (b-'0')<<3 | (c - '0'), true
}

// hasOption comprueba si opt aparece como token completo en la lista de
// opciones de montaje separada por comas.
func hasOption(options, opt string) bool {
	for _, o := range strings.Split(options, ",") {
		if o == opt {
			return true
		}
	}
	return false
}
