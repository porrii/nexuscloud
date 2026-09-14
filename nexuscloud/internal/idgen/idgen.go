// Package idgen genera identificadores y tokens aleatorios criptográficamente
// seguros. Toda entidad expuesta externamente (usuarios, sesiones,
// invitaciones, archivos) usa estos identificadores en vez de IDs
// incrementales, para no permitir enumeración ni IDOR (§123, §198-199).
package idgen

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// New genera un UUID v4 aleatorio.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("idgen: crypto/rand no disponible: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // versión 4
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Token genera un token opaco de 256 bits de entropía, codificado en
// base64url sin padding. Se usa para sesiones, invitaciones y tokens de API;
// solo su hash SHA-256 se persiste en base de datos (§26, §78, §172).
func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("idgen: generando token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
