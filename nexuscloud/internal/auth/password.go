// Package auth implementa autenticación: hashing de contraseñas (§25),
// sesiones (§26), TOTP (§25) e invitaciones (§21). Depende de
// internal/users para resolver identidades, nunca al revés.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/porrii/nexuscloud/internal/config"
)

var ErrInvalidPassword = errors.New("auth: contraseña incorrecta")

const saltLen = 16

// Hasher produce y verifica hashes Argon2id (§25). Los parámetros de coste
// se embeben en cada hash (formato similar a PHC) para poder endurecerlos
// en el futuro sin invalidar contraseñas ya almacenadas.
type Hasher struct {
	params config.Argon2Config
}

func NewHasher(params config.Argon2Config) *Hasher {
	return &Hasher{params: params}
}

// Hash produce: $argon2id$v=19$m=<KiB>,t=<iter>,p=<par>$<salt-b64>$<hash-b64>
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generando salt: %w", err)
	}
	sum := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, 32)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

// Verify no depende de los parámetros actuales de h: usa los que están
// embebidos en encodedHash, para que una contraseña siga siendo válida
// aunque security.argon2 cambie después en config.yaml.
func (h *Hasher) Verify(password, encodedHash string) error {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrInvalidPassword
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidPassword
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return ErrInvalidPassword
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrInvalidPassword
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidPassword
	}

	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidPassword
	}
	return nil
}
