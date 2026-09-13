//go:build windows

package snapshotinfo

import (
	"testing"
	"time"
)

// El caso vacío usa el JSON REAL devuelto por
// "Get-CimInstance Win32_ShadowCopy | ConvertTo-Json -InputObject @(...)"
// en esta máquina Windows real sin ninguna instantánea existente
// (capturado durante el diseño de este slice, no adivinado).
func TestParseShadowCopiesEmptyRealCapture(t *testing.T) {
	const realEmptyOutput = `[

]`
	snaps, err := parseShadowCopies([]byte(realEmptyOutput))
	if err != nil {
		t.Fatalf("parseShadowCopies: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("esperaba 0 instantáneas, got %d: %+v", len(snaps), snaps)
	}
}

func TestParseShadowCopiesWithRealWmiDateFormat(t *testing.T) {
	// Formato confirmado contra Win32_OperatingSystem.InstallDate en esta
	// misma máquina: "/Date(ms-desde-epoch-unix)/", no ISO-8601.
	const content = `[
  {
    "ID": "{B2C3D4E5-0000-0000-0000-000000000001}",
    "VolumeName": "\\\\?\\Volume{11111111-2222-3333-4444-555555555555}\\",
    "InstallDate": "/Date(1621845782000)/",
    "Persistent": true
  }
]`
	snaps, err := parseShadowCopies([]byte(content))
	if err != nil {
		t.Fatalf("parseShadowCopies: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 instantánea, got %d: %+v", len(snaps), snaps)
	}
	s := snaps[0]
	if s.ID != "{B2C3D4E5-0000-0000-0000-000000000001}" {
		t.Errorf("ID mal parseado: %+v", s)
	}
	if !s.Persistent {
		t.Errorf("esperaba Persistent=true: %+v", s)
	}
	want := time.UnixMilli(1621845782000)
	if !s.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, esperado %v", s.CreatedAt, want)
	}
}

func TestParseShadowCopiesUnrecognizedDateDoesNotDropTheSnapshot(t *testing.T) {
	const content = `[
  {
    "ID": "{X}",
    "VolumeName": "C:\\",
    "InstallDate": "algo-que-no-es-una-fecha-wmi",
    "Persistent": false
  }
]`
	snaps, err := parseShadowCopies([]byte(content))
	if err != nil {
		t.Fatalf("parseShadowCopies: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 instantánea igualmente (con CreatedAt cero), got %d: %+v", len(snaps), snaps)
	}
	if !snaps[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt debía quedar en cero ante un formato no reconocido: %+v", snaps[0])
	}
}

func TestParseShadowCopiesGarbageNeverPanics(t *testing.T) {
	if _, err := parseShadowCopies([]byte("esto no es JSON")); err == nil {
		t.Error("esperaba un error al parsear JSON inválido, no un panic ni un resultado silencioso")
	}
	if _, err := parseShadowCopies([]byte("")); err == nil {
		t.Error("esperaba un error al parsear contenido vacío")
	}
}

func TestParseWmiDate(t *testing.T) {
	cases := []struct {
		in      string
		wantOK  bool
		wantSec int64
	}{
		{"/Date(1621845782000)/", true, 1621845782},
		{"/Date(0)/", true, 0},
		{"/Date(-1000)/", true, -1},
		{"2026-09-12T00:00:00Z", false, 0},
		{"/Date(abc)/", false, 0},
		{"", false, 0},
	}
	for _, tc := range cases {
		got, ok := parseWmiDate(tc.in)
		if ok != tc.wantOK {
			t.Errorf("parseWmiDate(%q) ok = %v, esperado %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && got.Unix() != tc.wantSec {
			t.Errorf("parseWmiDate(%q) = %v (unix=%d), esperado unix=%d", tc.in, got, got.Unix(), tc.wantSec)
		}
	}
}
