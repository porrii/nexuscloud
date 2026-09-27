package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
	"github.com/porrii/nexuscloud/internal/idgen"
)

type SQLThumbnailJobRepository struct {
	conn *db.Conn
}

func NewSQLThumbnailJobRepository(conn *db.Conn) *SQLThumbnailJobRepository {
	return &SQLThumbnailJobRepository{conn: conn}
}

// UpsertPending sigue el mismo patrón que SQLFileRepository.UpsertFile
// (ADR-031): MySQL/MariaDB no tienen "ON CONFLICT ... DO UPDATE", solo
// "ON DUPLICATE KEY UPDATE" -- con file_id como única columna UNIQUE de la
// tabla además de la PK, son equivalentes en la práctica. Reinicia a
// pending/attempts=0 si el job ya existía (resubir un archivo no debe
// dejar un job en failed colgado para siempre).
func (r *SQLThumbnailJobRepository) UpsertPending(ctx context.Context, fileID, sha256Hex string, kind ThumbnailKind) (*ThumbnailJob, error) {
	now := db.TimeToString(time.Now().UTC())
	query := `
		INSERT INTO thumbnail_jobs (id, file_id, sha256, kind, status, attempts, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'pending', 0, NULL, ?, ?)
		ON CONFLICT (file_id)
		DO UPDATE SET sha256 = excluded.sha256, kind = excluded.kind, status = 'pending', attempts = 0, last_error = NULL, updated_at = excluded.updated_at`
	if r.conn.Driver == "mysql" {
		query = `
			INSERT INTO thumbnail_jobs (id, file_id, sha256, kind, status, attempts, last_error, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'pending', 0, NULL, ?, ?)
			ON DUPLICATE KEY UPDATE sha256 = VALUES(sha256), kind = VALUES(kind), status = 'pending', attempts = 0, last_error = NULL, updated_at = VALUES(updated_at)`
	}
	if _, err := r.conn.ExecContext(ctx, query, idgen.New(), fileID, sha256Hex, string(kind), now, now); err != nil {
		return nil, fmt.Errorf("creando job de miniatura: %w", err)
	}

	// El ON CONFLICT puede haber conservado el id/created_at de una fila
	// preexistente: releemos por clave natural (file_id) para devolver
	// siempre el estado real persistido, mismo criterio que UpsertFile.
	row := r.conn.QueryRowContext(ctx, thumbnailJobSelectColumns+` WHERE file_id = ?`, fileID)
	job, err := scanThumbnailJobRow(row)
	if err != nil {
		return nil, fmt.Errorf("releyendo job de miniatura tras guardar: %w", err)
	}
	return job, nil
}

// GetByFileID lee el job tal cual está, sin tocar attempts/status --
// nunca hace un upsert. ErrThumbnailJobNotFound si no existe ninguno.
func (r *SQLThumbnailJobRepository) GetByFileID(ctx context.Context, fileID string) (*ThumbnailJob, error) {
	row := r.conn.QueryRowContext(ctx, thumbnailJobSelectColumns+` WHERE file_id = ?`, fileID)
	job, err := scanThumbnailJobRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrThumbnailJobNotFound
		}
		return nil, fmt.Errorf("buscando job de miniatura por archivo: %w", err)
	}
	return job, nil
}

// NextPending consume la cola secuencialmente (sin worker pool, §34
// Decisión 1): el job pendiente más antiguo, o nil si no hay ninguno.
func (r *SQLThumbnailJobRepository) NextPending(ctx context.Context) (*ThumbnailJob, error) {
	row := r.conn.QueryRowContext(ctx, thumbnailJobSelectColumns+
		` WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1`)
	job, err := scanThumbnailJobRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("buscando siguiente job de miniatura: %w", err)
	}
	return job, nil
}

func (r *SQLThumbnailJobRepository) MarkDone(ctx context.Context, id string) error {
	if _, err := r.conn.ExecContext(ctx, `DELETE FROM thumbnail_jobs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("borrando job de miniatura completado: %w", err)
	}
	return nil
}

// MarkFailedAttempt incrementa attempts de forma atómica (una sola
// sentencia, sin leer-antes-de-escribir) y decide status con el mismo
// UPDATE -- CASE WHEN es SQL estándar, se comporta igual en los 3
// motores. Relee el estado resultante para reportar becameFailed con la
// verdad persistida, no con un cálculo en Go que podría desincronizarse
// bajo escritura concurrente.
func (r *SQLThumbnailJobRepository) MarkFailedAttempt(ctx context.Context, id, lastError string, maxAttempts int) (bool, error) {
	now := db.TimeToString(time.Now().UTC())
	_, err := r.conn.ExecContext(ctx, `
		UPDATE thumbnail_jobs
		SET attempts = attempts + 1,
		    last_error = ?,
		    status = CASE WHEN attempts + 1 >= ? THEN 'failed' ELSE 'pending' END,
		    updated_at = ?
		WHERE id = ?`,
		lastError, maxAttempts, now, id)
	if err != nil {
		return false, fmt.Errorf("registrando fallo de miniatura: %w", err)
	}
	var status string
	if err := r.conn.QueryRowContext(ctx, `SELECT status FROM thumbnail_jobs WHERE id = ?`, id).Scan(&status); err != nil {
		return false, fmt.Errorf("releyendo estado del job tras el fallo: %w", err)
	}
	return status == string(ThumbnailJobFailed), nil
}

func (r *SQLThumbnailJobRepository) ListByStatus(ctx context.Context, status ThumbnailJobStatus, limit, offset int) ([]*ThumbnailJob, error) {
	// limit<=0 (típicamente el 0 de strconv.Atoi("") cuando el llamador HTTP
	// no pasó ?limit=) significa "usa un valor por defecto razonable", NUNCA
	// "LIMIT 0" -- en SQL eso devuelve cero filas siempre, mismo bug que ya
	// se evitó en audit.SQLRepository.ListEvents (mismo límite/tope, 100/500,
	// por consistencia).
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.conn.QueryContext(ctx, thumbnailJobSelectColumns+
		` WHERE status = ? ORDER BY created_at ASC LIMIT ? OFFSET ?`, string(status), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listando jobs de miniatura: %w", err)
	}
	defer rows.Close()

	var out []*ThumbnailJob
	for rows.Next() {
		job, err := scanThumbnailJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

const thumbnailJobSelectColumns = `SELECT id, file_id, sha256, kind, status, attempts, last_error, created_at, updated_at FROM thumbnail_jobs`

func scanThumbnailJobRow(row rowScanner) (*ThumbnailJob, error) {
	var (
		j                    ThumbnailJob
		kind, status         string
		lastError            sql.NullString
		createdAt, updatedAt string
	)
	if err := row.Scan(&j.ID, &j.FileID, &j.SHA256, &kind, &status, &j.Attempts, &lastError, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	j.Kind = ThumbnailKind(kind)
	j.Status = ThumbnailJobStatus(status)
	if lastError.Valid {
		j.LastError = lastError.String
	}
	var err error
	if j.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	if j.UpdatedAt, err = db.StringToTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parseando updated_at: %w", err)
	}
	return &j, nil
}
