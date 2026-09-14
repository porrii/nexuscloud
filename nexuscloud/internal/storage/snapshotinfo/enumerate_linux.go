//go:build linux

package snapshotinfo

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// enumerate en Linux agrega ambas vías conocidas (ZFS y Btrfs). Ninguna de
// las dos falla nunca "hacia fuera": cada una ya degrada a una lista vacía
// internamente si su herramienta no está instalada o no encuentra nada, así
// que enumerate en sí nunca devuelve error -- una de las dos puede no estar
// disponible sin que eso oculte lo que la otra sí encuentre.
func enumerate(ctx context.Context) ([]Snapshot, error) {
	var snaps []Snapshot
	if zfsSnaps, err := enumerateZfs(ctx); err == nil {
		snaps = append(snaps, zfsSnaps...)
	}
	if btrfsSnaps, err := enumerateBtrfs(ctx); err == nil {
		snaps = append(snaps, btrfsSnaps...)
	}
	return snaps, nil
}

// --- ZFS -------------------------------------------------------------

// enumerateZfs detecta snapshots ZFS vía el binario "zfs" -- no hay un
// pseudo-fichero equivalente a /proc/mdstat para listar snapshots ZFS (el
// estado vive detrás de una ioctl privada del kernel module), así que, a
// diferencia de raidinfo en Linux, aquí hace falta invocar el propio
// comando, igual que los adaptadores Windows de este mismo paquete y de
// raidinfo.
func enumerateZfs(ctx context.Context) ([]Snapshot, error) {
	if _, err := exec.LookPath("zfs"); err != nil {
		// ZFS no instalado en este sistema -- no es "sin adaptador para
		// Linux" (Linux SÍ tiene uno, este mismo); es "esta vía concreta no
		// tiene nada que reportar aquí".
		return nil, nil
	}

	// -H: sin cabecera, campos separados por TAB (formato estable pensado
	// para parseo, no el formato humano de columnas alineadas). -p:
	// valores "parseable" -- creation sale como epoch Unix entero, nunca
	// como fecha humana (evita cualquier ambigüedad de formato de fecha,
	// lección directa del hallazgo de /Date(ms)/ en el adaptador Windows
	// de este mismo paquete). -o: columnas explícitas y en orden fijo.
	out, err := exec.CommandContext(ctx, "zfs", "list", "-H", "-p", "-t", "snapshot", "-o", "name,creation").Output()
	if err != nil {
		// zfs sin ningún pool importado sale con código de salida != 0
		// ("no datasets available") -- un sistema real sin pools, no un
		// fallo de NexusCloud. Mismo criterio de "degradar a vacío, nunca
		// a error" que /proc/mdstat ausente en raidinfo.
		return nil, nil
	}
	return parseZfsSnapshots(out), nil
}

// parseZfsSnapshots interpreta la salida de
// "zfs list -H -p -t snapshot -o name,creation" -- separada de la
// invocación real para poder probarla con texto sintético, mismo criterio
// que parseMdstat/parseVirtualDisks/parseShadowCopies en este proyecto.
// Nunca falla: una línea con menos campos de los esperados se ignora sin
// interrumpir el resto.
func parseZfsSnapshots(output []byte) []Snapshot {
	var snaps []Snapshot
	sc := bufio.NewScanner(bytes.NewReader(output))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		volumeName := name
		if i := strings.Index(name, "@"); i >= 0 {
			volumeName = name[:i]
		}
		snap := Snapshot{ID: name, VolumeName: volumeName, Persistent: true}
		if sec, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			snap.CreatedAt = time.Unix(sec, 0)
		}
		snaps = append(snaps, snap)
	}
	return snaps
}

// --- Btrfs -------------------------------------------------------------

// enumerateBtrfs detecta snapshots Btrfs (subvolúmenes con la propiedad de
// solo lectura) vía el binario "btrfs". A diferencia de "zfs list" (que
// opera globalmente sobre todos los pools importados), "btrfs subvolume
// list" exige la ruta de un filesystem Btrfs ya montado -- por eso primero
// hace falta encontrar los montajes Btrfs vía /proc/mounts.
func enumerateBtrfs(ctx context.Context) ([]Snapshot, error) {
	if _, err := exec.LookPath("btrfs"); err != nil {
		return nil, nil
	}
	mountpoints, err := btrfsMountpoints()
	if err != nil {
		return nil, nil
	}

	var snaps []Snapshot
	for _, mp := range mountpoints {
		out, err := exec.CommandContext(ctx, "btrfs", "subvolume", "list", "-s", mp).Output()
		if err != nil {
			continue // este montaje concreto falló (permisos, etc.) -- se prueban los demás
		}
		snaps = append(snaps, parseBtrfsSubvolumes(out, mp)...)
	}
	return snaps, nil
}

// btrfsMountpoints relee /proc/mounts buscando puntos de montaje con
// fstype "btrfs" -- lectura mínima e independiente, sin importar
// internal/storage/diskinfo (mismo criterio de paquetes hermanos sin
// dependencia entre sí ya establecido en este proyecto): aquí solo hacen
// falta las columnas 2 (punto de montaje) y 3 (fstype) de cada línea, nada
// del resto del análisis que sí necesita diskinfo (tamaños, exclusión de
// pseudo-filesystems, etc. -- un pseudo-fs nunca será fstype "btrfs", así
// que ese filtro no hace falta aquí).
func btrfsMountpoints() ([]string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseBtrfsMountpoints(f)
}

// parseBtrfsMountpoints hace el trabajo real de btrfsMountpoints, separado
// de la apertura del fichero para poder probarlo con texto sintético --
// mismo criterio que el resto de parsers de este proyecto.
func parseBtrfsMountpoints(r io.Reader) ([]string, error) {
	var mounts []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[2] != "btrfs" {
			continue
		}
		mounts = append(mounts, fields[1])
	}
	return mounts, sc.Err()
}

// parseBtrfsSubvolumes interpreta la salida de
// "btrfs subvolume list -s <mountpoint>" -- separada de la invocación real
// para poder probarla con texto sintético. A diferencia de ZFS, este
// formato no tiene un modo "parseable": es texto separado por espacios
// pensado para humanos, p.ej.
// "ID 261 gen 100 cgen 98 top level 5 otime 2026-09-01 10:00:00 path @snapshots/root-20260901".
// El ID numérico interno de btrfs no se usa como Snapshot.ID (no es un
// identificador globalmente significativo por sí solo, a diferencia del
// nombre completo de un snapshot ZFS) -- en su lugar, ID combina el
// mountpoint con la ruta del subvolumen, que es lo que un administrador
// reconocería. Se asume que "path" es siempre el ÚLTIMO campo con nombre
// -- todo lo que sigue al token literal "path" se une de nuevo como la
// ruta completa, sea cual sea su contenido (btrfs-progs no escapa espacios
// en el nombre). Una línea sin "path" reconocible se ignora sin
// interrumpir el resto -- nunca panic.
func parseBtrfsSubvolumes(output []byte, mountpoint string) []Snapshot {
	var snaps []Snapshot
	sc := bufio.NewScanner(bytes.NewReader(output))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		var path string
		var createdAt time.Time
		for i := 0; i < len(fields); i++ {
			switch fields[i] {
			case "otime":
				if i+2 < len(fields) {
					ts := fields[i+1] + " " + fields[i+2]
					if t, err := time.ParseInLocation("2006-01-02 15:04:05", ts, time.Local); err == nil {
						createdAt = t
					}
				}
			case "path":
				if i+1 < len(fields) {
					path = strings.Join(fields[i+1:], " ")
				}
				i = len(fields) // "path" es siempre el último campo: termina el escaneo de esta línea
			}
		}
		if path == "" {
			continue
		}
		snaps = append(snaps, Snapshot{
			ID:         mountpoint + ":" + path,
			VolumeName: mountpoint,
			CreatedAt:  createdAt,
			Persistent: true,
		})
	}
	return snaps
}
