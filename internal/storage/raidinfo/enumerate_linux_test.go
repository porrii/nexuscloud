//go:build linux

package raidinfo

import (
	"strings"
	"testing"
)

func TestParseMdstatHealthyRaid1(t *testing.T) {
	const content = `Personalities : [raid1] [raid6] [raid5] [raid4] [raid0] [raid10]
md0 : active raid1 sdb1[1] sda1[0]
      976630464 blocks super 1.2 [2/2] [UU]

unused devices: <none>
`
	arrays, err := parseMdstat(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	if len(arrays) != 1 {
		t.Fatalf("esperaba 1 array, got %d: %+v", len(arrays), arrays)
	}
	a := arrays[0]
	if a.Name != "md0" || a.Level != "raid1" || !a.Active {
		t.Errorf("cabecera mal parseada: %+v", a)
	}
	if a.TotalDevices != 2 || a.ActiveDevices != 2 || a.Degraded {
		t.Errorf("estado mal parseado: %+v", a)
	}
	if a.Recovering {
		t.Errorf("no debería estar en recuperación: %+v", a)
	}
	if len(a.Devices) != 2 {
		t.Fatalf("esperaba 2 dispositivos, got %d: %+v", len(a.Devices), a.Devices)
	}
	if a.Devices[0].Name != "sdb1" || a.Devices[0].Role != 1 || a.Devices[0].Faulty {
		t.Errorf("dispositivo 0 mal parseado: %+v", a.Devices[0])
	}
	if a.Devices[1].Name != "sda1" || a.Devices[1].Role != 0 {
		t.Errorf("dispositivo 1 mal parseado: %+v", a.Devices[1])
	}
}

func TestParseMdstatHealthyRaid5IgnoresLevelSpecificExtras(t *testing.T) {
	const content = `Personalities : [raid6] [raid5] [raid4]
md1 : active raid5 sdc1[2] sdb1[1] sda1[0]
      1953260544 blocks super 1.2 level 5, 512k chunk, algorithm 2 [3/3] [UUU]

unused devices: <none>
`
	arrays, err := parseMdstat(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	if len(arrays) != 1 {
		t.Fatalf("esperaba 1 array, got %d: %+v", len(arrays), arrays)
	}
	a := arrays[0]
	if a.Level != "raid5" {
		t.Errorf("Level = %q, esperado raid5", a.Level)
	}
	if a.TotalDevices != 3 || a.ActiveDevices != 3 || a.Degraded {
		t.Errorf("los campos extra de nivel (super/chunk/algorithm) debían ignorarse sin romper el conteo: %+v", a)
	}
	if len(a.Devices) != 3 {
		t.Fatalf("esperaba 3 dispositivos, got %d: %+v", len(a.Devices), a.Devices)
	}
}

func TestParseMdstatDegradedWithFaultyDevice(t *testing.T) {
	const content = `Personalities : [raid1]
md0 : active raid1 sdb1[1](F) sda1[0]
      976630464 blocks super 1.2 [2/1] [U_]

unused devices: <none>
`
	arrays, err := parseMdstat(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	if len(arrays) != 1 {
		t.Fatalf("esperaba 1 array, got %d: %+v", len(arrays), arrays)
	}
	a := arrays[0]
	if !a.Degraded {
		t.Errorf("esperaba Degraded=true (2/1, patrón U_): %+v", a)
	}
	if a.TotalDevices != 2 || a.ActiveDevices != 1 {
		t.Errorf("conteo mal parseado: %+v", a)
	}
	if len(a.Devices) != 2 || !a.Devices[0].Faulty {
		t.Fatalf("esperaba el primer dispositivo marcado Faulty: %+v", a.Devices)
	}
	if a.Devices[1].Faulty {
		t.Errorf("el segundo dispositivo no debía estar marcado Faulty: %+v", a.Devices[1])
	}
}

func TestParseMdstatRecoveryInProgress(t *testing.T) {
	const content = `Personalities : [raid1]
md0 : active raid1 sdb1[1] sda1[0]
      976630464 blocks super 1.2 [2/2] [UU]
      [==========>..........]  recovery = 52.3% (511234/976630464) finish=45.2min speed=10234K/sec

unused devices: <none>
`
	arrays, err := parseMdstat(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	if len(arrays) != 1 {
		t.Fatalf("esperaba 1 array, got %d: %+v", len(arrays), arrays)
	}
	a := arrays[0]
	if !a.Recovering {
		t.Fatalf("esperaba Recovering=true: %+v", a)
	}
	if a.RecoveryPct != 52.3 {
		t.Errorf("RecoveryPct = %v, esperado 52.3", a.RecoveryPct)
	}
}

func TestParseMdstatNoArraysConfigured(t *testing.T) {
	const content = `Personalities :
unused devices: <none>
`
	arrays, err := parseMdstat(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	if len(arrays) != 0 {
		t.Errorf("esperaba 0 arrays, got %d: %+v", len(arrays), arrays)
	}
}

func TestParseMdstatMultipleArraysAndGarbageNeverPanics(t *testing.T) {
	const content = `Personalities : [raid1] [raid5]
md0 : active raid1 sdb1[1] sda1[0]
      976630464 blocks super 1.2 [2/2] [UU]

esto no es una línea reconocida de mdstat
md1 : active raid5 sdc1[2] sdb1[1] sda1[0]
      1953260544 blocks super 1.2 [3/3] [UUU]

unused devices: <none>
`
	arrays, err := parseMdstat(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseMdstat: %v", err)
	}
	if len(arrays) != 2 {
		t.Fatalf("esperaba 2 arrays pese a la línea basura intermedia, got %d: %+v", len(arrays), arrays)
	}

	// Contenido totalmente vacío: nunca debe panicar, siempre lista vacía.
	empty, err := parseMdstat(strings.NewReader(""))
	if err != nil || len(empty) != 0 {
		t.Errorf("parseMdstat(\"\") = %v, %v; esperado ([], nil)", empty, err)
	}
}
