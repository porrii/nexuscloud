package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLayoutResolvesAndCreatesRoots(t *testing.T) {
	base := t.TempDir()
	p := LayoutParams{
		UserData:   filepath.Join(base, "storage"),
		Thumbnails: filepath.Join(base, "thumbnails"),
		Versions:   filepath.Join(base, "versions"),
		Temp:       filepath.Join(base, "tmp"),
	}
	l, err := NewLayout(p)
	if err != nil {
		t.Fatalf("NewLayout falló: %v", err)
	}
	for name, dir := range map[string]string{
		"userData": l.UserData, "thumbnails": l.Thumbnails,
		"versions": l.Versions, "temp": l.Temp,
	} {
		if !filepath.IsAbs(dir) {
			t.Errorf("%s = %q no es absoluta", name, dir)
		}
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			t.Errorf("%s = %q no se creó como directorio: %v", name, dir, err)
		}
	}
}

func TestNewLayoutIsIdempotent(t *testing.T) {
	base := t.TempDir()
	p := LayoutParams{
		UserData:   filepath.Join(base, "s"),
		Thumbnails: filepath.Join(base, "t"),
		Versions:   filepath.Join(base, "v"),
		Temp:       filepath.Join(base, "tmp"),
	}
	first, err := NewLayout(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewLayout(p)
	if err != nil {
		t.Fatal(err)
	}
	if *first != *second {
		t.Errorf("NewLayout no es idempotente: %+v vs %+v", *first, *second)
	}
}

func TestNewLayoutResolvesSymlinkedRoot(t *testing.T) {
	real := filepath.Join(t.TempDir(), "disco")
	if err := os.MkdirAll(real, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "montaje")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no se pueden crear symlinks en este entorno: %v", err)
	}
	base := t.TempDir()
	l, err := NewLayout(LayoutParams{
		UserData:   link,
		Thumbnails: filepath.Join(base, "t"),
		Versions:   filepath.Join(base, "v"),
		Temp:       filepath.Join(base, "tmp"),
	})
	if err != nil {
		t.Fatalf("NewLayout con userData symlink falló: %v", err)
	}
	want, _ := filepath.EvalSymlinks(real)
	if l.UserData != want {
		t.Errorf("l.UserData = %q, esperado %q (symlink de la raíz resuelto una vez)", l.UserData, want)
	}
}
