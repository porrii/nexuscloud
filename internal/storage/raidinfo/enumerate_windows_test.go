//go:build windows

package raidinfo

import "testing"

// El primer caso usa el JSON REAL devuelto por
// "Get-VirtualDisk | ConvertTo-Json -InputObject @(...)" en una máquina
// Windows 10 real sin ningún Virtual Disk configurado (capturado durante
// el diseño de este slice, no adivinado) -- confirma que el envoltorio
// @(...) produce "[]" y no "null" cuando no hay resultados.
func TestParseVirtualDisksEmptyRealCapture(t *testing.T) {
	const realEmptyOutput = `[

]`
	arrays, err := parseVirtualDisks([]byte(realEmptyOutput))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 0 {
		t.Errorf("esperaba 0 arrays, got %d: %+v", len(arrays), arrays)
	}
}

func TestParseVirtualDisksHealthyMirror(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Mirror",
    "OperationalStatus": "OK",
    "HealthStatus": "Healthy"
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 {
		t.Fatalf("esperaba 1 array, got %d: %+v", len(arrays), arrays)
	}
	a := arrays[0]
	if a.Name != "Datos" || a.Level != "Mirror" || !a.Active {
		t.Errorf("mal parseado: %+v", a)
	}
	if a.Degraded || a.Recovering {
		t.Errorf("esperaba sano y sin reconstrucción: %+v", a)
	}
}

func TestParseVirtualDisksDegradedMirror(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Mirror",
    "OperationalStatus": "OK",
    "HealthStatus": "Warning"
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 || !arrays[0].Degraded {
		t.Fatalf("esperaba Degraded=true (HealthStatus=Warning): %+v", arrays)
	}
}

func TestParseVirtualDisksRepairingIsRecovering(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Parity",
    "OperationalStatus": "Repairing",
    "HealthStatus": "Warning"
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 || !arrays[0].Recovering {
		t.Fatalf("esperaba Recovering=true (OperationalStatus=Repairing): %+v", arrays)
	}
	if arrays[0].RecoveryPct != 0 {
		t.Errorf("RecoveryPct debía quedar en 0 en este slice (sin Get-StorageJob): %+v", arrays[0])
	}
}

func TestParseVirtualDisksMultipleAndGarbageNeverPanics(t *testing.T) {
	const content = `[
  {"FriendlyName":"A","ResiliencySettingName":"Mirror","OperationalStatus":"OK","HealthStatus":"Healthy"},
  {"FriendlyName":"B","ResiliencySettingName":"Simple","OperationalStatus":"OK","HealthStatus":"Healthy"}
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil || len(arrays) != 2 {
		t.Fatalf("parseVirtualDisks = %v, %v; esperado 2 arrays sin error", arrays, err)
	}

	if _, err := parseVirtualDisks([]byte("esto no es JSON")); err == nil {
		t.Error("esperaba un error al parsear JSON inválido, no un panic ni un resultado silencioso")
	}
	if _, err := parseVirtualDisks([]byte("")); err == nil {
		t.Error("esperaba un error al parsear contenido vacío")
	}
}
