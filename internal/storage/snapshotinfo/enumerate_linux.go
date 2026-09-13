//go:build linux

package snapshotinfo

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// enumerate en Linux detecta snapshots ZFS vía el binario "zfs" -- no hay
// un pseudo-fichero equivalente a /proc/mdstat para listar snapshots ZFS
// (el estado vive detrás de una ioctl privada del kernel module), así que,
// a diferencia de raidinfo en Linux, aquí hace falta invocar el propio
// comando, igual que los adaptadores Windows de este mismo paquete y de
// raidinfo.
func enumerate(ctx context.Context) ([]Snapshot, error) {
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
