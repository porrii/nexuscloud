package storage

import (
	"context"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

// Usage es la huella en disco de un propietario (ADR-036): lo que ocupan sus
// archivos activos, los que están en la papelera y las versiones anteriores.
// Las tres cosas están de verdad en los pools, así que las tres cuentan para
// la cuota; el desglose existe para que la interfaz pueda explicar en qué se
// va el espacio (y por qué borrar no lo libera hasta vaciar la papelera).
type Usage struct {
	FilesBytes    int64
	TrashBytes    int64
	VersionsBytes int64
}

// Total es la huella completa: lo que se compara con la cuota.
func (u Usage) Total() int64 { return u.FilesBytes + u.TrashBytes + u.VersionsBytes }

// UsageRepository mide el uso. Se calcula con SUM en el momento (no con
// contadores): es autoritativo por construcción y no puede desincronizarse de
// los archivos reales tras una purga, una restauración o un fallo a medias.
type UsageRepository interface {
	// OwnerUsage devuelve la huella de un propietario (cero si no tiene datos).
	OwnerUsage(ctx context.Context, ownerID string) (Usage, error)
	// AllOwnersUsage devuelve la huella de todos los propietarios que tienen
	// algún dato, por ID de propietario (para el informe de la CLI).
	AllOwnersUsage(ctx context.Context) (map[string]Usage, error)
}

type SQLUsageRepository struct {
	conn *db.Conn
}

func NewSQLUsageRepository(conn *db.Conn) *SQLUsageRepository {
	return &SQLUsageRepository{conn: conn}
}

// castType es el tipo entero al que se convierte cada SUM: en PostgreSQL SUM
// de un BIGINT da NUMERIC y en MySQL DECIMAL, y hay que devolver un entero.
// Sale de un conjunto fijo según el driver: nunca de datos del usuario.
func (r *SQLUsageRepository) castType() string {
	if r.conn.Driver == "mysql" {
		return "SIGNED"
	}
	return "BIGINT"
}

// filesTable es la tabla de archivos tal y como se consulta para sumar tamaños.
// El índice de cobertura idx_files_owner_usage (migración 0011) resuelve la
// suma sin leer las filas, pero el optimizador de MySQL, por sí solo, sigue
// eligiendo idx_files_owner y tarda 9 veces más (650 frente a 73 ms con 200 000
// archivos de un propietario): ahí se le pide con FORCE INDEX.
func (r *SQLUsageRepository) filesTable() string {
	if r.conn.Driver == "mysql" {
		return "files FORCE INDEX (idx_files_owner_usage)"
	}
	return "files"
}

func (r *SQLUsageRepository) OwnerUsage(ctx context.Context, ownerID string) (Usage, error) {
	t := r.castType()
	var u Usage
	err := r.conn.QueryRowContext(ctx, `
		SELECT CAST(COALESCE(SUM(CASE WHEN deleted_at IS NULL THEN size_bytes ELSE 0 END), 0) AS `+t+`),
		       CAST(COALESCE(SUM(CASE WHEN deleted_at IS NOT NULL THEN size_bytes ELSE 0 END), 0) AS `+t+`)
		FROM `+r.filesTable()+` WHERE owner_id = ?`, ownerID).Scan(&u.FilesBytes, &u.TrashBytes)
	if err != nil {
		return Usage{}, fmt.Errorf("midiendo los archivos del propietario: %w", err)
	}
	// Sin JOIN: file_versions lleva el propietario (migración 0012), así que es una
	// sola lectura del índice (owner_id, size_bytes).
	err = r.conn.QueryRowContext(ctx, `
		SELECT CAST(COALESCE(SUM(size_bytes), 0) AS `+t+`)
		FROM file_versions WHERE owner_id = ?`, ownerID).Scan(&u.VersionsBytes)
	if err != nil {
		return Usage{}, fmt.Errorf("midiendo las versiones del propietario: %w", err)
	}
	return u, nil
}

func (r *SQLUsageRepository) AllOwnersUsage(ctx context.Context) (map[string]Usage, error) {
	t := r.castType()
	out := make(map[string]Usage)

	rows, err := r.conn.QueryContext(ctx, `
		SELECT owner_id,
		       CAST(COALESCE(SUM(CASE WHEN deleted_at IS NULL THEN size_bytes ELSE 0 END), 0) AS `+t+`),
		       CAST(COALESCE(SUM(CASE WHEN deleted_at IS NOT NULL THEN size_bytes ELSE 0 END), 0) AS `+t+`)
		FROM `+r.filesTable()+` GROUP BY owner_id`)
	if err != nil {
		return nil, fmt.Errorf("midiendo los archivos de todos los propietarios: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var u Usage
		if err := rows.Scan(&owner, &u.FilesBytes, &u.TrashBytes); err != nil {
			return nil, err
		}
		out[owner] = u
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	vrows, err := r.conn.QueryContext(ctx, `
		SELECT owner_id, CAST(COALESCE(SUM(size_bytes), 0) AS `+t+`)
		FROM file_versions GROUP BY owner_id`)
	if err != nil {
		return nil, fmt.Errorf("midiendo las versiones de todos los propietarios: %w", err)
	}
	defer vrows.Close()
	for vrows.Next() {
		var owner string
		var versions int64
		if err := vrows.Scan(&owner, &versions); err != nil {
			return nil, err
		}
		u := out[owner]
		u.VersionsBytes = versions
		out[owner] = u
	}
	return out, vrows.Err()
}
