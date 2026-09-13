//go:build linux

package snapshotinfo

import (
	"strings"
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

// --- Btrfs --------------------------------------------------------------

func TestParseBtrfsSubvolumesSingle(t *testing.T) {
	const output = "ID 261 gen 100 cgen 98 top level 5 otime 2026-09-01 10:00:00 path @snapshots/root-20260901\n"
	snaps := parseBtrfsSubvolumes([]byte(output), "/mnt/data")
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 snapshot, got %d: %+v", len(snaps), snaps)
	}
	s := snaps[0]
	if s.ID != "/mnt/data:@snapshots/root-20260901" {
		t.Errorf("ID = %q, esperado \"/mnt/data:@snapshots/root-20260901\"", s.ID)
	}
	if s.VolumeName != "/mnt/data" {
		t.Errorf("VolumeName = %q, esperado \"/mnt/data\"", s.VolumeName)
	}
	if !s.Persistent {
		t.Errorf("un snapshot Btrfs siempre debe ser Persistent=true: %+v", s)
	}
	want := time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local)
	if !s.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, esperado %v", s.CreatedAt, want)
	}
}

func TestParseBtrfsSubvolumesMultipleLines(t *testing.T) {
	const output = "ID 261 gen 100 cgen 98 top level 5 otime 2026-09-01 10:00:00 path @snapshots/root-20260901\n" +
		"ID 262 gen 105 cgen 102 top level 5 otime 2026-09-02 10:00:00 path @snapshots/root-20260902\n"
	snaps := parseBtrfsSubvolumes([]byte(output), "/mnt/data")
	if len(snaps) != 2 {
		t.Fatalf("esperaba 2 snapshots, got %d: %+v", len(snaps), snaps)
	}
}

func TestParseBtrfsSubvolumesPathWithSpacesTakesEverythingAfterPathToken(t *testing.T) {
	const output = "ID 261 gen 100 cgen 98 top level 5 otime 2026-09-01 10:00:00 path @snapshots/copia de seguridad\n"
	snaps := parseBtrfsSubvolumes([]byte(output), "/mnt/data")
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 snapshot, got %d: %+v", len(snaps), snaps)
	}
	if snaps[0].ID != "/mnt/data:@snapshots/copia de seguridad" {
		t.Errorf("ID = %q, esperado que \"path\" tome todo lo que sigue, espacios incluidos", snaps[0].ID)
	}
}

func TestParseBtrfsSubvolumesLineWithoutPathIgnoredWithoutPanic(t *testing.T) {
	const output = "ID 261 gen 100 cgen 98 top level 5 otime 2026-09-01 10:00:00\n" +
		"ID 262 gen 105 cgen 102 top level 5 otime 2026-09-02 10:00:00 path @snapshots/root-20260902\n"
	snaps := parseBtrfsSubvolumes([]byte(output), "/mnt/data")
	if len(snaps) != 1 {
		t.Fatalf("esperaba 1 snapshot válido (la línea sin \"path\" se ignora), got %d: %+v", len(snaps), snaps)
	}
}

func TestParseBtrfsSubvolumesEmptyOutput(t *testing.T) {
	snaps := parseBtrfsSubvolumes([]byte(""), "/mnt/data")
	if len(snaps) != 0 {
		t.Errorf("esperaba 0 snapshots, got %d: %+v", len(snaps), snaps)
	}
}

func TestParseBtrfsMountpointsFiltersFstype(t *testing.T) {
	// Mismo formato de /proc/mounts que ya prueba diskinfo, pero aquí solo
	// se necesitan las columnas 2 y 3.
	const mounts = `proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
/dev/sda1 / ext4 rw,relatime 0 0
/dev/sdb1 /mnt/data btrfs rw,relatime,space_cache 0 0
/dev/sdc1 /mnt/otro btrfs rw,relatime 0 0
`
	found, err := parseBtrfsMountpoints(strings.NewReader(mounts))
	if err != nil {
		t.Fatalf("parseBtrfsMountpoints: %v", err)
	}
	if len(found) != 2 || found[0] != "/mnt/data" || found[1] != "/mnt/otro" {
		t.Errorf("montajes btrfs mal filtrados: %+v", found)
	}
}

func TestParseBtrfsMountpointsNoneFound(t *testing.T) {
	const mounts = "/dev/sda1 / ext4 rw,relatime 0 0\n"
	found, err := parseBtrfsMountpoints(strings.NewReader(mounts))
	if err != nil {
		t.Fatalf("parseBtrfsMountpoints: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("esperaba 0 montajes btrfs, got %+v", found)
	}
}
