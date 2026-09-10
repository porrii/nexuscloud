//go:build linux

package diskinfo

import (
	"strings"
	"testing"
)

func TestParseMountLinesFiltersPseudoAndDedups(t *testing.T) {
	const mounts = `proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
sysfs /sys sysfs rw,nosuid,nodev,noexec,relatime 0 0
/dev/sda1 / ext4 rw,relatime 0 0
tmpfs /run tmpfs rw,nosuid,nodev,size=1638400k 0 0
/dev/sdb1 /mnt/datos xfs ro,relatime 0 0
/dev/sda1 / ext4 rw,relatime 0 0
/dev/sdc1 /mnt/con\040espacio btrfs rw,relatime 0 0
overlay /var/lib/docker/overlay2/x/merged overlay rw 0 0
`
	entries, err := parseMountLines(strings.NewReader(mounts))
	if err != nil {
		t.Fatalf("parseMountLines: %v", err)
	}

	got := map[string]mountEntry{}
	for _, e := range entries {
		got[e.mountPoint] = e
	}

	if len(entries) != 3 {
		t.Fatalf("esperaba 3 entradas reales, got %d: %+v", len(entries), entries)
	}
	if _, ok := got["/proc"]; ok {
		t.Error("no debería incluir /proc (pseudo-fs)")
	}
	if _, ok := got["/run"]; ok {
		t.Error("no debería incluir /run (tmpfs)")
	}
	if _, ok := got["/var/lib/docker/overlay2/x/merged"]; ok {
		t.Error("no debería incluir un overlay de contenedor")
	}
	root, ok := got["/"]
	if !ok {
		t.Fatal("faltaba la raíz /")
	}
	if root.fsType != "ext4" || root.device != "/dev/sda1" || root.readOnly {
		t.Errorf("raíz mal parseada: %+v", root)
	}
	datos, ok := got["/mnt/datos"]
	if !ok || !datos.readOnly {
		t.Errorf("/mnt/datos debería estar y ser readOnly: %+v", datos)
	}
	if _, ok := got["/mnt/con espacio"]; !ok {
		t.Error("el escape octal \\040 (espacio) no se deshizo en el punto de montaje")
	}
}

func TestUnescapeMountField(t *testing.T) {
	cases := map[string]string{
		`/simple/path`:    `/simple/path`,
		`/con\040espacio`: `/con espacio`,
		`/tab\011aqui`:    "/tab\taqui",
		`/back\134slash`:  `/back\slash`,
		`\040\040`:        `  `,
		`sin-escape\x`:    `sin-escape\x`, // \x no es octal: se deja tal cual
	}
	for in, want := range cases {
		if got := unescapeMountField(in); got != want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasOption(t *testing.T) {
	if !hasOption("rw,nosuid,ro,relatime", "ro") {
		t.Error("ro debería detectarse como token completo")
	}
	if hasOption("rw,errors=remount-ro", "ro") {
		t.Error("'ro' no debería casar dentro de 'remount-ro'")
	}
}
