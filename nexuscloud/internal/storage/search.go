package storage

import (
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

// SearchFilters agrupa los criterios de búsqueda de metadatos (§33): nombre,
// ruta, tipo, extensión, fecha y tamaño. Struct abierto a propósito -- un
// futuro filtro de búsqueda de CONTENIDO (que el spec solo pide "preparar
// arquitectura para", no implementar ya) se añadiría aquí como un campo más,
// sin romper ninguna firma existente.
type SearchFilters struct {
	// Query es una subcadena de Name O ParentPath, siempre comparada en
	// minúsculas (case-insensitive en los 3 motores vía LOWER(...) LIKE
	// LOWER(...) -- más simple y predecible que depender de ILIKE de
	// Postgres o del collation de cada tabla en MySQL). Vacío = sin filtro.
	Query string
	// MimeType es un PREFIJO (p.ej. "image/" para cualquier imagen), nunca
	// una igualdad exacta -- así "tipo: imagen" cubre image/jpeg, image/png,
	// etc. sin enumerarlos. Vacío = sin filtro. No aplica a carpetas.
	MimeType string
	// Ext es la extensión exacta SIN el punto (p.ej. "pdf"), comparada
	// sobre el nombre del archivo. Vacío = sin filtro. No aplica a carpetas.
	Ext string
	// DateFrom/DateTo acotan CreatedAt (inclusive en ambos extremos). nil =
	// sin ese extremo.
	DateFrom, DateTo *time.Time
	// SizeMin/SizeMax acotan SizeBytes (inclusive). nil = sin ese extremo.
	// No aplica a carpetas.
	SizeMin, SizeMax *int64
}

// searchConditions construye los fragmentos WHERE y argumentos comunes a
// files y directories (Query sobre name/parent_path, DateFrom/DateTo sobre
// created_at) -- cada repositorio añade encima sus propias condiciones
// (owner_id, deleted_at, y en FileRepository también mime_type/ext/tamaño)
// antes de unir todo con AND. Query se escapa con escapeLikePattern (mismo
// helper que MoveDirectoryTree) para que un % o _ literal en lo que alguien
// busca no se interprete como comodín SQL.
func searchConditions(f SearchFilters) (conds []string, args []any) {
	if f.Query != "" {
		like := "%" + escapeLikePattern(f.Query) + "%"
		conds = append(conds, `(LOWER(name) LIKE LOWER(?) ESCAPE '\' OR LOWER(parent_path) LIKE LOWER(?) ESCAPE '\')`)
		args = append(args, like, like)
	}
	if f.DateFrom != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, db.TimeToString(*f.DateFrom))
	}
	if f.DateTo != nil {
		conds = append(conds, "created_at <= ?")
		args = append(args, db.TimeToString(*f.DateTo))
	}
	return conds, args
}
