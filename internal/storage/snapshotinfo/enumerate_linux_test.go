//go:build linux

package snapshotinfo

import (
	"testing"
	"time"
)

func TestParseZfsSnapshotsSingle(t *testing.T) {
	const output = "tank/data@daily-2026-09-01\t1756742400\n"
	snaps := parseZfsSnapshots([]byte(output))
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 snapshot, got %d: %+v", len(snaps), snaps)
	}
	s := snaps[0]
	if s.ID != "tank/data@daily-2026-09-01" {
		t.Errorf("ID = %q, esperado el nombre completo", s.ID)
	}
	if s.VolumeName != "tank/data" {
		t.Errorf("VolumeName = %q, esperado \"tank/data\"", s.VolumeName)
	}
	if !s.Persistent {
		t.Errorf("un snapshot ZFS siempre debe ser Persistent=true: %+v", s)
	}
	want := time.Unix(1756742400, 0)
	if !s.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, esperado %v", s.CreatedAt, want)
	}
}

func TestParseZfsSnapshotsMultipleDatasetsAndPools(t *testing.T) {
	const output = "tank/data@daily-2026-09-01\t1756742400\n" +
		"tank/data@daily-2026-09-02\t1756828800\n" +
		"rpool/ROOT/debian@install\t1609459200\n"
	snaps := parseZfsSnapshots([]byte(output))
	if len(snaps) != 3 {
		t.Fatalf("esperaba 3 snapshots, got %d: %+v", len(snaps), snaps)
	}
	if snaps[2].VolumeName != "rpool/ROOT/debian" {
		t.Errorf("VolumeName con varios niveles mal parseado: %+v", snaps[2])
	}
}

func TestParseZfsSnapshotsMalformedLineIgnoredWithoutPanic(t *testing.T) {
	const output = "tank/data@daily-2026-09-01\t1756742400\n" +
		"esto-no-tiene-tab-ni-creation\n" +
		"tank/data@daily-2026-09-02\t1756828800\n"
	snaps := parseZfsSnapshots([]byte(output))
	if len(snaps) != 2 {
		t.Fatalf("esperaba 2 snapshots válidos (la línea malformada se ignora), got %d: %+v", len(snaps), snaps)
	}
}

func TestParseZfsSnapshotsEmptyOutput(t *testing.T) {
	snaps := parseZfsSnapshots([]byte(""))
	if len(snaps) != 0 {
		t.Errorf("esperaba 0 snapshots, got %d: %+v", len(snaps), snaps)
	}
}

func TestParseZfsSnapshotsUnparsableCreationLeavesZeroTime(t *testing.T) {
	const output = "tank/data@x\tno-es-un-epoch\n"
	snaps := parseZfsSnapshots([]byte(output))
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 snapshot igualmente, got %d: %+v", len(snaps), snaps)
	}
	if !snaps[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt debía quedar en cero ante un epoch no parseable: %+v", snaps[0])
	}
}
