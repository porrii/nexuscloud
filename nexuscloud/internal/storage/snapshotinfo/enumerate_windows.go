//go:build windows

package snapshotinfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// enumerate en Windows consulta VSS (Volume Shadow Copy Service) vía
// PowerShell -- mismo criterio que internal/storage/raidinfo para Storage
// Spaces: VSS se gestiona por WMI/CIM, sin una API Win32 clásica más
// simple, y PowerShell es la interfaz que Microsoft documenta y mantiene.
//
// Deliberadamente NO se usa "vssadmin list shadowstorage" (que daría el
// espacio reservado por volumen): exige privilegios de administrador
// elevados, confirmado contra una instalación real -- no se van a pedir
// privilegios elevados para un comando de solo lectura. Get-CimInstance
// Win32_ShadowCopy sí funciona sin elevación y es suficiente para "detectar
// si hay snapshots" (§17).
func enumerate(ctx context.Context) ([]Snapshot, error) {
	const script = `ConvertTo-Json -InputObject @(Get-CimInstance -ClassName Win32_ShadowCopy -ErrorAction SilentlyContinue | ` +
		`Select-Object ID,VolumeName,InstallDate,Persistent)`

	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, ErrUnsupported
	}
	snaps, err := parseShadowCopies(out)
	if err != nil {
		return nil, ErrUnsupported
	}
	return snaps, nil
}

// shadowCopy es la forma mínima que se le pide a PowerShell -- ver
// Select-Object en el script de arriba.
type shadowCopy struct {
	ID          string `json:"ID"`
	VolumeName  string `json:"VolumeName"`
	InstallDate string `json:"InstallDate"`
	Persistent  bool   `json:"Persistent"`
}

// parseShadowCopies decodifica la salida de Get-CimInstance
// Win32_ShadowCopy (ver enumerate) y la mapea a Snapshot. Separada de la
// invocación real a powershell.exe para poder probarla con JSON sintético,
// mismo criterio que parseMdstat/parseVirtualDisks en raidinfo.
func parseShadowCopies(data []byte) ([]Snapshot, error) {
	var copies []shadowCopy
	if err := json.Unmarshal(data, &copies); err != nil {
		return nil, err
	}
	snaps := make([]Snapshot, 0, len(copies))
	for _, c := range copies {
		s := Snapshot{ID: c.ID, VolumeName: c.VolumeName, Persistent: c.Persistent}
		// Un InstallDate en un formato no reconocido deja CreatedAt en su
		// valor cero en vez de descartar el snapshot entero -- un snapshot
		// sin fecha legible sigue siendo información útil.
		if t, ok := parseWmiDate(c.InstallDate); ok {
			s.CreatedAt = t
		}
		snaps = append(snaps, s)
	}
	return snaps, nil
}

// wmiDateRe reconoce el formato heredado que ConvertTo-Json usa para
// serializar campos DateTime de WMI: "/Date(<ms-desde-epoch-unix>)/" --
// NO es ISO-8601. Confirmado contra Win32_OperatingSystem.InstallDate en
// una instalación Windows real durante el diseño de este slice (
// Win32_ShadowCopy no tenía ninguna instancia real contra la que probarlo
// directamente en ese momento).
var wmiDateRe = regexp.MustCompile(`^/Date\((-?\d+)\)/$`)

func parseWmiDate(s string) (time.Time, bool) {
	m := wmiDateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	ms, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}
