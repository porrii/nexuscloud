package storage

import "strings"

// isUniqueViolation reconoce el mensaje de violación de restricción UNIQUE
// tanto de sqlite (modernc.org/sqlite) como de postgres (pgx), evitando una
// dependencia directa en los tipos de error internos de cada driver. Misma
// lógica que internal/users, duplicada a propósito: no vale la pena una
// dependencia cruzada entre paquetes de dominio por una función de dos
// líneas.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key")
}
