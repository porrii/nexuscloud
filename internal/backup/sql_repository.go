package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLRepository struct {
	conn *db.Conn
}

func NewSQLRepository(conn *db.Conn) *SQLRepository {
	return &SQLRepository{conn: conn}
}

const jobColumns = `id, status, destination_path, pool_ids_json, file_count, total_bytes, started_at, finished_at, error_message`

const jobSelectColumns = `SELECT ` + jobColumns + ` FROM backup_jobs`

func (r *SQLRepository) CreateJob(ctx context.Context, j *Job) error {
	poolIDsJSON, err := json.Marshal(j.PoolIDs)
	if err != nil {
		return fmt.Errorf("serializando pool_ids: %w", err)
	}
	_, err = r.conn.ExecContext(ctx, `
		INSERT INTO backup_jobs
			(id, status, destination_path, pool_ids_json, file_count, total_bytes, started_at, finished_at, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Status, j.DestinationPath, string(poolIDsJSON), j.FileCount, j.TotalBytes,
		db.TimeToString(j.StartedAt), nil, "")
	if err != nil {
		return fmt.Errorf("creando backup job: %w", err)
	}
	return nil
}

func (r *SQLRepository) FinishJob(ctx context.Context, id, status string, fileCount, totalBytes int64, errorMessage string) error {
	res, err := r.conn.ExecContext(ctx, `
		UPDATE backup_jobs
		SET status = ?, file_count = ?, total_bytes = ?, finished_at = ?, error_message = ?
		WHERE id = ?`,
		status, fileCount, totalBytes, db.TimeToString(time.Now().UTC()), errorMessage, id)
	if err != nil {
		return fmt.Errorf("finalizando backup job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

func (r *SQLRepository) GetJobByID(ctx context.Context, id string) (*Job, error) {
	row := r.conn.QueryRowContext(ctx, jobSelectColumns+` WHERE id = ?`, id)
	j, err := scanJobRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return j, nil
}

func (r *SQLRepository) ListJobs(ctx context.Context) ([]*Job, error) {
	rows, err := r.conn.QueryContext(ctx, jobSelectColumns+` ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listando backup jobs: %w", err)
	}
	defer rows.Close()

	var out []*Job
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r *SQLRepository) DeleteJob(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM backup_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("borrando backup job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobRow(row rowScanner) (*Job, error) {
	var j Job
	var poolIDsJSON, startedAt string
	var finishedAt sql.NullString
	if err := row.Scan(
		&j.ID, &j.Status, &j.DestinationPath, &poolIDsJSON, &j.FileCount, &j.TotalBytes,
		&startedAt, &finishedAt, &j.ErrorMessage,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(poolIDsJSON), &j.PoolIDs); err != nil {
		return nil, fmt.Errorf("deserializando pool_ids: %w", err)
	}
	var err error
	if j.StartedAt, err = db.StringToTime(startedAt); err != nil {
		return nil, fmt.Errorf("parseando started_at: %w", err)
	}
	if j.FinishedAt, err = db.ParseNullableTime(finishedAt.String, finishedAt.Valid); err != nil {
		return nil, fmt.Errorf("parseando finished_at: %w", err)
	}
	return &j, nil
}
