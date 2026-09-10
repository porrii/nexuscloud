package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLPoolRepository struct {
	conn *db.Conn
}

func NewSQLPoolRepository(conn *db.Conn) *SQLPoolRepository {
	return &SQLPoolRepository{conn: conn}
}

const poolColumns = `id, name, type, path, priority, status, created_at,
	utilization_policy, backup_policy, versioning_policy, snapshot_policy`

const poolSelectColumns = `SELECT ` + poolColumns + ` FROM storage_pools`

func (r *SQLPoolRepository) CreatePool(ctx context.Context, p *Pool) error {
	p.defaultPolicies()
	if err := p.validatePolicies(); err != nil {
		return err
	}
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO storage_pools
			(id, name, type, path, priority, status, created_at,
			 utilization_policy, backup_policy, versioning_policy, snapshot_policy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Type, p.Path, p.Priority, p.Status, db.TimeToString(p.CreatedAt),
		p.UtilizationPolicy, p.BackupPolicy, p.VersioningPolicy, p.SnapshotPolicy)
	if err != nil {
		return fmt.Errorf("creando storage pool: %w", err)
	}
	return nil
}

func (r *SQLPoolRepository) GetPoolByID(ctx context.Context, id string) (*Pool, error) {
	row := r.conn.QueryRowContext(ctx, poolSelectColumns+` WHERE id = ?`, id)
	p, err := scanPool(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *SQLPoolRepository) ListPools(ctx context.Context) ([]*Pool, error) {
	rows, err := r.conn.QueryContext(ctx, poolSelectColumns+` ORDER BY priority, created_at`)
	if err != nil {
		return nil, fmt.Errorf("listando storage pools: %w", err)
	}
	defer rows.Close()

	var out []*Pool
	for rows.Next() {
		p, err := scanPoolRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SQLPoolRepository) DefaultPool(ctx context.Context) (*Pool, error) {
	row := r.conn.QueryRowContext(ctx, poolSelectColumns+` WHERE status = 'active' ORDER BY priority, created_at LIMIT 1`)
	p, err := scanPool(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPoolNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *SQLPoolRepository) UpdatePool(ctx context.Context, p *Pool) error {
	if err := p.validatePolicies(); err != nil {
		return err
	}
	res, err := r.conn.ExecContext(ctx, `
		UPDATE storage_pools
		SET name = ?, priority = ?,
		    utilization_policy = ?, backup_policy = ?, versioning_policy = ?, snapshot_policy = ?
		WHERE id = ?`,
		p.Name, p.Priority,
		p.UtilizationPolicy, p.BackupPolicy, p.VersioningPolicy, p.SnapshotPolicy,
		p.ID)
	if err != nil {
		return fmt.Errorf("actualizando storage pool: %w", err)
	}
	return requireAffected(res, ErrPoolNotFound)
}

func (r *SQLPoolRepository) SetPoolStatus(ctx context.Context, id, status string) error {
	if status != "active" && status != "disabled" {
		return fmt.Errorf("%w: status=%q", ErrInvalidPoolPolicy, status)
	}
	res, err := r.conn.ExecContext(ctx,
		`UPDATE storage_pools SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("cambiando estado del storage pool: %w", err)
	}
	return requireAffected(res, ErrPoolNotFound)
}

func (r *SQLPoolRepository) DeletePool(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM storage_pools WHERE id = ?`, id)
	if err != nil {
		// files.pool_id / directories.pool_id son FK ON DELETE RESTRICT:
		// el motor rechaza el borrado si hay contenido en el pool.
		if isForeignKeyViolation(err) {
			return ErrPoolInUse
		}
		return fmt.Errorf("borrando storage pool: %w", err)
	}
	return requireAffected(res, ErrPoolNotFound)
}

func requireAffected(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}

// isForeignKeyViolation reconoce el error de FK tanto de modernc.org/sqlite
// ("FOREIGN KEY constraint failed") como de pgx ("violates foreign key
// constraint" / SQLSTATE 23503).
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key") || strings.Contains(msg, "23503")
}

func scanPool(row *sql.Row) (*Pool, error) { return scanPoolRow(row) }

func scanPoolRow(row rowScanner) (*Pool, error) {
	var p Pool
	var createdAt string
	if err := row.Scan(
		&p.ID, &p.Name, &p.Type, &p.Path, &p.Priority, &p.Status, &createdAt,
		&p.UtilizationPolicy, &p.BackupPolicy, &p.VersioningPolicy, &p.SnapshotPolicy,
	); err != nil {
		return nil, err
	}
	var err error
	if p.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	return &p, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}
