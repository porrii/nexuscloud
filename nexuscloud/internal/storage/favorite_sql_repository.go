package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLFavoriteRepository struct {
	conn *db.Conn
}

func NewSQLFavoriteRepository(conn *db.Conn) *SQLFavoriteRepository {
	return &SQLFavoriteRepository{conn: conn}
}

// CreateFavorite traduce una violación de UNIQUE(user_id, file_id)/
// UNIQUE(user_id, directory_id) a ErrFavoriteAlreadyExists: quien llama
// (FileService.AddFavorite) decide si eso es idempotencia deseada.
func (r *SQLFavoriteRepository) CreateFavorite(ctx context.Context, f *Favorite) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO favorites (id, user_id, file_id, directory_id, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		f.ID, f.UserID, nullableStr(f.FileID), nullableStr(f.DirectoryID), db.TimeToString(f.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrFavoriteAlreadyExists
		}
		return fmt.Errorf("creando favorito: %w", err)
	}
	return nil
}

// GetFavoriteByResource resuelve el favorito de userID sobre un recurso
// concreto, si existe -- usado por CreateFavorite (capa de servicio) para
// devolver el existente ante ErrFavoriteAlreadyExists.
func (r *SQLFavoriteRepository) GetFavoriteByResource(ctx context.Context, userID string, isDirectory bool, resourceID string) (*Favorite, error) {
	column := "file_id"
	if isDirectory {
		column = "directory_id"
	}
	// column sale de un booleano, nunca de datos del usuario: no hay riesgo
	// de inyección al interpolarlo (mismo criterio que SQLUsageRepository.castType).
	row := r.conn.QueryRowContext(ctx, favoriteSelectColumns+` WHERE user_id = ? AND `+column+` = ?`, userID, resourceID)
	f, err := scanFavoriteRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrFavoriteNotFound
		}
		return nil, err
	}
	return f, nil
}

// RemoveFavorite exige coincidencia de userID en la propia cláusula WHERE
// (§198 IDOR), mismo criterio que el resto del proyecto.
func (r *SQLFavoriteRepository) RemoveFavorite(ctx context.Context, id, userID string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM favorites WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("quitando favorito: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrFavoriteNotFound
	}
	return nil
}

func (r *SQLFavoriteRepository) ListFavoritesForUser(ctx context.Context, userID string) ([]*Favorite, error) {
	rows, err := r.conn.QueryContext(ctx, favoriteSelectColumns+` WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listando favoritos: %w", err)
	}
	defer rows.Close()

	var out []*Favorite
	for rows.Next() {
		f, err := scanFavoriteRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

const favoriteSelectColumns = `SELECT id, user_id, file_id, directory_id, created_at FROM favorites`

func scanFavoriteRow(row rowScanner) (*Favorite, error) {
	var (
		f                   Favorite
		fileID, directoryID sql.NullString
		createdAt           string
	)
	if err := row.Scan(&f.ID, &f.UserID, &fileID, &directoryID, &createdAt); err != nil {
		return nil, err
	}
	if fileID.Valid {
		f.FileID = fileID.String
	}
	if directoryID.Valid {
		f.DirectoryID = directoryID.String
	}
	var err error
	if f.CreatedAt, err = db.StringToTime(createdAt); err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	return &f, nil
}
