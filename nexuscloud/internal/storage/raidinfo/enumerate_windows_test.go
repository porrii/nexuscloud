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

// TestParseVirtualDisksRepairingWithoutJobDataLeavesRecoveryPctZero cubre
// el caso "Recovering=true pero sin ningún MSFT_StorageJob correlacionado"
// (p.ej. el job ya terminó justo entre las dos consultas, o PowerShell no
// pudo correlacionarlo) -- RecoveryPct debe quedar en 0 en vez de inventar
// un progreso, nunca al revés (ver TestParseVirtualDisksRecoveringWithJobUsesPercentComplete
// para el caso con datos de job reales).
func TestParseVirtualDisksRepairingWithoutJobDataLeavesRecoveryPctZero(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Parity",
    "OperationalStatus": "Repairing",
    "HealthStatus": "Warning",
    "PhysicalDisks": [],
    "Jobs": []
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
		t.Errorf("RecoveryPct debía quedar en 0 sin ningún job correlacionado: %+v", arrays[0])
	}
}

// TestParseVirtualDisksRecoveringWithJobUsesPercentComplete confirma la
// correlación positiva (ADR-027): con Recovering=true y al menos un
// MSFT_StorageJob correlacionado, RecoveryPct refleja su PercentComplete.
func TestParseVirtualDisksRecoveringWithJobUsesPercentComplete(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Parity",
    "OperationalStatus": "Repairing",
    "HealthStatus": "Warning",
    "PhysicalDisks": [],
    "Jobs": [{"PercentComplete": 42.5}]
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 || arrays[0].RecoveryPct != 42.5 {
		t.Fatalf("esperaba RecoveryPct=42.5 (del job correlacionado): %+v", arrays)
	}
}

// TestParseVirtualDisksJobPresentButNotRecoveringLeavesRecoveryPctZero
// confirma que la condición manda sobre la mera presencia de un job: un
// MSFT_StorageJob puede representar otra operación (p.ej. optimización)
// sin que el array esté realmente reconstruyéndose.
func TestParseVirtualDisksJobPresentButNotRecoveringLeavesRecoveryPctZero(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Mirror",
    "OperationalStatus": "OK",
    "HealthStatus": "Healthy",
    "PhysicalDisks": [],
    "Jobs": [{"PercentComplete": 77}]
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 || arrays[0].Recovering || arrays[0].RecoveryPct != 0 {
		t.Fatalf("un job presente sin Recovering=true no debía producir ningún RecoveryPct: %+v", arrays)
	}
}

// TestParseVirtualDisksCorrelatesPhysicalDisksAndMarksFaulty confirma
// Devices/TotalDevices/ActiveDevices poblados desde Get-PhysicalDisk, y
// que un disco con HealthStatus distinto de Healthy cuenta como Faulty y
// reduce ActiveDevices.
func TestParseVirtualDisksCorrelatesPhysicalDisksAndMarksFaulty(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Mirror",
    "OperationalStatus": "OK",
    "HealthStatus": "Warning",
    "PhysicalDisks": [
      {"FriendlyName": "Disco1", "Usage": "Auto-Select", "HealthStatus": "Healthy"},
      {"FriendlyName": "Disco2", "Usage": "Auto-Select", "HealthStatus": "Unhealthy"},
      {"FriendlyName": "DiscoRepuesto", "Usage": "Hot Spare", "HealthStatus": "Healthy"}
    ],
    "Jobs": []
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 {
		t.Fatalf("esperaba 1 array, got %d", len(arrays))
	}
	a := arrays[0]
	if a.TotalDevices != 3 || a.ActiveDevices != 2 {
		t.Fatalf("TotalDevices/ActiveDevices = %d/%d, esperado 3/2 (un disco Unhealthy no cuenta como activo)", a.TotalDevices, a.ActiveDevices)
	}
	if len(a.Devices) != 3 {
		t.Fatalf("Devices = %+v, esperados 3", a.Devices)
	}
	if a.Devices[1].Name != "Disco2" || !a.Devices[1].Faulty {
		t.Errorf("Disco2 debía marcarse Faulty=true (HealthStatus=Unhealthy): %+v", a.Devices[1])
	}
	if a.Devices[2].Name != "DiscoRepuesto" || !a.Devices[2].Spare {
		t.Errorf("DiscoRepuesto debía marcarse Spare=true (Usage=Hot Spare): %+v", a.Devices[2])
	}
}

// TestParseVirtualDisksNullPhysicalDisksAndJobsNeverPanics confirma que
// PhysicalDisks/Jobs ausentes (JSON null, no solo array vacío -- lo que
// ConvertTo-Json produce cuando esa correlación no encontró nada) se
// decodifican como slices de Go en nil, recorridos igual que un slice
// vacío, sin ningún caso especial ni panic.
func TestParseVirtualDisksNullPhysicalDisksAndJobsNeverPanics(t *testing.T) {
	const content = `[
  {
    "FriendlyName": "Datos",
    "ResiliencySettingName": "Simple",
    "OperationalStatus": "OK",
    "HealthStatus": "Healthy",
    "PhysicalDisks": null,
    "Jobs": null
  }
]`
	arrays, err := parseVirtualDisks([]byte(content))
	if err != nil {
		t.Fatalf("parseVirtualDisks: %v", err)
	}
	if len(arrays) != 1 || arrays[0].TotalDevices != 0 || len(arrays[0].Devices) != 0 {
		t.Fatalf("esperaba 0 dispositivos sin panic: %+v", arrays)
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
