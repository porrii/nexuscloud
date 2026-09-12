//go:build linux

package raidinfo

import (
	"bufio"
	"context"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func enumerate(ctx context.Context) ([]RaidArray, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open("/proc/mdstat")
	if err != nil {
		if os.IsNotExist(err) {
			// El kernel no tiene el módulo md cargado -- no es un fallo de
			// NexusCloud, es "sin arrays" (distinto de ErrUnsupported, que
			// significa "esta plataforma no tiene adaptador").
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return parseMdstat(f)
}

var (
	headerRe   = regexp.MustCompile(`^(\S+)\s*:\s*(.+)$`)
	deviceRe   = regexp.MustCompile(`^(\S+?)\[(\d+)\](\([FS]\))?$`)
	bracketRe  = regexp.MustCompile(`\[[^\[\]]*\]`)
	countsRe   = regexp.MustCompile(`^(\d+)/(\d+)$`)
	upDownRe   = regexp.MustCompile(`^[U_]+$`)
	recoveryRe = regexp.MustCompile(`=\s*(\d+(?:\.\d+)?)%`)
)

// parseMdstat interpreta el formato de /proc/mdstat -- separado de la
// apertura del fichero real para poder probarlo con texto sintético
// (mismo criterio que parseMountLines en diskinfo). El formato es estable
// desde hace décadas; esta función es deliberadamente tolerante con
// cualquier campo intermedio que no reconozca (versión de superbloque,
// detalles específicos del nivel como "512k chunk, algorithm 2") en vez de
// exigir una gramática completa -- solo extrae las señales de seguridad
// que pide §12: identidad, dispositivos, degradado, en reconstrucción.
func parseMdstat(r io.Reader) ([]RaidArray, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	var arrays []RaidArray
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "Personalities") || strings.HasPrefix(line, "unused devices") || strings.TrimSpace(line) == "" {
			continue
		}
		m := headerRe.FindStringSubmatch(line)
		if m == nil {
			continue // línea no reconocida -- se ignora, nunca panic
		}
		arr := parseArrayHeader(m[1], m[2])

		// Línea de tamaño/estado ("NNN blocks ... [N/M] [U_..]"), siempre
		// la siguiente si el array está activo.
		if i+1 < len(lines) {
			i++
			applyStatusLine(&arr, lines[i])
		}
		// Línea de progreso opcional (resync/recovery en curso).
		if i+1 < len(lines) && looksLikeProgressLine(lines[i+1]) {
			i++
			applyProgressLine(&arr, lines[i])
		}

		arrays = append(arrays, arr)
	}
	return arrays, nil
}

func parseArrayHeader(name, rest string) RaidArray {
	arr := RaidArray{Name: name}
	fields := strings.Fields(rest)
	idx := 0
	if idx < len(fields) {
		arr.Active = fields[idx] == "active"
		idx++
	}
	// "(auto-read-only)" cuelga como token propio tras "active".
	if idx < len(fields) && strings.HasPrefix(fields[idx], "(") {
		idx++
	}
	// El nivel es el siguiente token, salvo que ya parezca un dispositivo
	// (algunos arrays "inactive" no muestran nivel en absoluto).
	if idx < len(fields) && !deviceRe.MatchString(fields[idx]) {
		arr.Level = fields[idx]
		idx++
	}
	for ; idx < len(fields); idx++ {
		dm := deviceRe.FindStringSubmatch(fields[idx])
		if dm == nil {
			continue
		}
		role, _ := strconv.Atoi(dm[2])
		arr.Devices = append(arr.Devices, RaidDevice{
			Name: dm[1], Role: role,
			Faulty: dm[3] == "(F)", Spare: dm[3] == "(S)",
		})
	}
	return arr
}

func applyStatusLine(arr *RaidArray, statusLine string) {
	brackets := bracketRe.FindAllString(statusLine, -1)
	if len(brackets) < 2 {
		return
	}
	counts := countsRe.FindStringSubmatch(strings.Trim(brackets[len(brackets)-2], "[]"))
	pattern := strings.Trim(brackets[len(brackets)-1], "[]")
	if counts == nil || !upDownRe.MatchString(pattern) {
		return
	}
	arr.TotalDevices, _ = strconv.Atoi(counts[1])
	arr.ActiveDevices, _ = strconv.Atoi(counts[2])
	arr.Degraded = arr.ActiveDevices < arr.TotalDevices || strings.Contains(pattern, "_")
}

func looksLikeProgressLine(line string) bool {
	return strings.Contains(line, "recovery =") || strings.Contains(line, "resync =")
}

func applyProgressLine(arr *RaidArray, line string) {
	arr.Recovering = true
	if m := recoveryRe.FindStringSubmatch(line); m != nil {
		arr.RecoveryPct, _ = strconv.ParseFloat(m[1], 64)
	}
}
