package auth

import (
	"strings"
	"testing"

	"github.com/porrii/nexuscloud/internal/config"
)

func testHasher() *Hasher {
	// Parámetros mínimos viables para que los tests corran rápido; la
	// configuración real usa los valores de config.Defaults() (§25).
	return NewHasher(config.Argon2Config{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1})
}

func TestHashAndVerifyRoundTrip(t *testing.T) {
	h := testHasher()
	hash, err := h.Hash("correcto-caballo-batería-grapa")
	if err != nil {
		t.Fatalf("Hash falló: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash no tiene el prefijo esperado: %s", hash)
	}
	if err := h.Verify("correcto-caballo-batería-grapa", hash); err != nil {
		t.Errorf("Verify con la contraseña correcta falló: %v", err)
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	h := testHasher()
	hash, err := h.Hash("contraseña-real")
	if err != nil {
		t.Fatalf("Hash falló: %v", err)
	}
	if err := h.Verify("contraseña-incorrecta", hash); err != ErrInvalidPassword {
		t.Errorf("err = %v, esperado ErrInvalidPassword", err)
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	h := testHasher()
	if err := h.Verify("cualquiera", "no-es-un-hash-valido"); err != ErrInvalidPassword {
		t.Errorf("err = %v, esperado ErrInvalidPassword", err)
	}
}

func TestVerifyHonorsParamsEmbeddedInHash(t *testing.T) {
	// Un hash generado con unos parámetros debe seguir siendo verificable
	// aunque el Hasher actual use otros distintos (§25: endurecer sin
	// invalidar contraseñas existentes).
	oldHasher := NewHasher(config.Argon2Config{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1})
	hash, err := oldHasher.Hash("password")
	if err != nil {
		t.Fatalf("Hash falló: %v", err)
	}

	newHasher := NewHasher(config.Argon2Config{MemoryKiB: 16 * 1024, Iterations: 2, Parallelism: 1})
	if err := newHasher.Verify("password", hash); err != nil {
		t.Errorf("Verify con parámetros distintos a los del hash original falló: %v", err)
	}
}
