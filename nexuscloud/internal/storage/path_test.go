package storage

import (
	"os"
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

func TestResolveRootFollowsRootSymlink(t *testing.T) {
	real := filepath.Join(t.TempDir(), "disco-real")
	if err := os.MkdirAll(real, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "enlace-raiz")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no se pueden crear symlinks en este entorno: %v", err)
	}
	got, err := ResolveRoot(link)
	if err != nil {
		t.Fatalf("ResolveRoot con raíz-symlink falló: %v", err)
	}
	want, _ := filepath.EvalSymlinks(real)
	if got != want {
		t.Errorf("ResolveRoot(%q) = %q, esperado %q (la raíz puede ser un symlink legítimo)", link, got, want)
	}
}

func TestSafeJoinResolvedAllowsOrdinaryPaths(t *testing.T) {
	root, err := ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := SafeJoinResolved(root, "Documentos/informe.pdf")
	if err != nil {
		t.Fatalf("SafeJoinResolved falló para una ruta normal: %v", err)
	}
	if want := filepath.Join(root, "Documentos", "informe.pdf"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSafeJoinResolvedRejectsSymlinkEscapingRoot(t *testing.T) {
	outside := t.TempDir() // objetivo fuera del pool
	root, err := ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(root, "evil")
	if err := os.Symlink(outside, evil); err != nil {
		t.Skipf("no se pueden crear symlinks en este entorno: %v", err)
	}

	// Objetivo existente detrás del symlink.
	if err := os.WriteFile(filepath.Join(outside, "secreto.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SafeJoinResolved(root, "evil/secreto.txt"); err != ErrPathEscapesRoot {
		t.Errorf("err = %v, esperado ErrPathEscapesRoot (symlink escapando con destino existente)", err)
	}
	// Objetivo INEXISTENTE detrás del symlink (symlink roto o fichero nuevo):
	// también debe rechazarse, aunque EvalSymlinks del destino falle.
	if _, err := SafeJoinResolved(root, "evil/fichero-nuevo.txt"); err != ErrPathEscapesRoot {
		t.Errorf("err = %v, esperado ErrPathEscapesRoot (symlink escapando con destino inexistente)", err)
	}
}

func TestSafeJoinResolvedRejectsAnySymlinkComponent(t *testing.T) {
	root, err := ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Symlink apuntando a otro sitio DENTRO del propio pool: aun así se
	// rechaza -- FileService nunca crea symlinks, así que cualquiera que
	// aparezca en el árbol de almacenamiento es sospechoso, apunte donde
	// apunte ("sin symlinks en el almacenamiento").
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
		t.Skipf("no se pueden crear symlinks en este entorno: %v", err)
	}
	if _, err := SafeJoinResolved(root, "alias/x.txt"); err != ErrPathEscapesRoot {
		t.Errorf("err = %v, esperado ErrPathEscapesRoot (symlink interno también se rechaza)", err)
	}
}

func TestSafeJoinResolvedStillBlocksLexicalTraversal(t *testing.T) {
	root, err := ResolveRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"../../etc/passwd", "foo/../../bar", "/../../etc/passwd"} {
		full, err := SafeJoinResolved(root, a)
		if err != nil {
			continue
		}
		if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
			t.Errorf("SafeJoinResolved(%q) escapó: %q", a, full)
		}
	}
}
