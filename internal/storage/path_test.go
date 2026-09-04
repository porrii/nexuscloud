package storage

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeJoinAllowsOrdinaryPaths(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "Documentos/informe.pdf")
	if err != nil {
		t.Fatalf("SafeJoin falló para una ruta normal: %v", err)
	}
	want := filepath.Join(root, "Documentos", "informe.pdf")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSafeJoinBlocksPathTraversal(t *testing.T) {
	root := t.TempDir()
	attacks := []string{
		"../../../etc/passwd",
		"../../etc/passwd",
		"..\\..\\Windows\\System32\\config\\SAM",
		"foo/../../bar",
		"/../../etc/passwd",
		"....//....//etc/passwd",
	}
	for _, a := range attacks {
		full, err := SafeJoin(root, a)
		if err != nil {
			continue // rechazado explícitamente: correcto
		}
		// Si no fue rechazado, el resultado DEBE seguir dentro de root.
		rootAbs, _ := filepath.Abs(root)
		if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
			t.Errorf("SafeJoin(%q) escapó de la raíz: %q", a, full)
		}
	}
}

func TestSafeJoinRejectsNullByte(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "archivo\x00.txt"); err != ErrInvalidPath {
		t.Errorf("err = %v, esperado ErrInvalidPath para un byte nulo", err)
	}
}

func TestSafeJoinNeverEscapesRootAcrossManyVariants(t *testing.T) {
	root := t.TempDir()
	rootAbs, _ := filepath.Abs(root)
	inputs := []string{
		"a/b/c", "..", "../..", "a/../../b", "./a/./b/.", "//a//b", "a/b/../../../../../../etc/passwd",
	}
	for _, in := range inputs {
		full, err := SafeJoin(root, in)
		if err != nil {
			continue
		}
		if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
			t.Errorf("input %q escapó de root: %q", in, full)
		}
	}
}
