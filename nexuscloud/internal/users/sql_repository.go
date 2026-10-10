package users

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLRepository struct {
	conn *db.Conn
}

func NewSQLRepository(conn *db.Conn) *SQLRepository {
	return &SQLRepository{conn: conn}
}

func (r *SQLRepository) CreateUser(ctx context.Context, u *User) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO users (id, username, display_name, email, password_hash, status, quota_bytes, totp_secret, created_at, updated_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, nullStr(u.Email), u.PasswordHash, string(u.Status),
		nullInt64(u.QuotaBytes), nullStr(u.TOTPSecret),
		db.TimeToString(u.CreatedAt), db.TimeToString(u.UpdatedAt), db.NullableTimeToString(u.LastLoginAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("creando usuario: %w", err)
	}
	return nil
}

func (r *SQLRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := r.conn.QueryRowContext(ctx, userSelectColumns+` WHERE id = ?`, id)
	return scanUser(row)
}

func (r *SQLRepository) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := r.conn.QueryRowContext(ctx, userSelectColumns+` WHERE username = ?`, username)
	return scanUser(row)
}

func (r *SQLRepository) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := r.conn.QueryContext(ctx, userSelectColumns+` ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("listando usuarios: %w", err)
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *SQLRepository) UpdateUser(ctx context.Context, u *User) error {
	res, err := r.conn.ExecContext(ctx, `
		UPDATE users SET display_name = ?, email = ?, password_hash = ?, status = ?,
			quota_bytes = ?, totp_secret = ?, updated_at = ?, last_login_at = ?
		WHERE id = ?`,
		u.DisplayName, nullStr(u.Email), u.PasswordHash, string(u.Status),
		nullInt64(u.QuotaBytes), nullStr(u.TOTPSecret), db.TimeToString(u.UpdatedAt), db.NullableTimeToString(u.LastLoginAt),
		u.ID,
	)
	if err != nil {
		return fmt.Errorf("actualizando usuario: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *SQLRepository) DeleteUser(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("eliminando usuario: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *SQLRepository) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := r.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("contando usuarios: %w", err)
	}
	return n, nil
}

func (r *SQLRepository) AssignRole(ctx context.Context, userID, roleID string) error {
	_, err := r.conn.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`, userID, roleID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil // idempotente: ya tenía el rol
		}
		return fmt.Errorf("asignando rol: %w", err)
	}
	return nil
}

func (r *SQLRepository) RolesForUser(ctx context.Context, userID string) ([]Role, error) {
	rows, err := r.conn.QueryContext(ctx, `
		SELECT r.id, r.name FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = ? ORDER BY r.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("consultando roles: %w", err)
	}
	defer rows.Close()

	var out []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (r *SQLRepository) HasRole(ctx context.Context, userID, roleID string) (bool, error) {
	var n int
	err := r.conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ?`, userID, roleID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("comprobando rol: %w", err)
	}
	return n > 0, nil
}

// SetRole deja a userID con UN solo rol, roleID (ADR-042 Decisión 8), en
// una transacción. Si con el cambio el usuario deja de ser super_admin,
// comprueba DENTRO de la misma transacción que queda al menos otro
// super_admin activo (ErrLastSuperAdmin si no). En PostgreSQL/MySQL con
// READ COMMITTED dos degradaciones simultáneas de los dos últimos
// super_admin podrían cruzarse; es una operación manual y rara, y el
// servicio vuelve a comprobarlo antes de llamar aquí.
func (r *SQLRepository) SetRole(ctx context.Context, userID, roleID string) error {
	tx, err := r.conn.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("iniciando transacción: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("buscando usuario: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	if roleID != RoleSuperAdmin {
		var wasSuper, others int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM user_roles WHERE user_id = ? AND role_id = ?`, userID, RoleSuperAdmin).Scan(&wasSuper); err != nil {
			return fmt.Errorf("comprobando rol actual: %w", err)
		}
		if wasSuper > 0 {
			if err := tx.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id
				WHERE ur.role_id = ? AND u.status = ? AND u.id <> ?`,
				RoleSuperAdmin, string(StatusActive), userID).Scan(&others); err != nil {
				return fmt.Errorf("contando super_admin: %w", err)
			}
			if others == 0 {
				return ErrLastSuperAdmin
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("quitando roles: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`, userID, roleID); err != nil {
		return fmt.Errorf("asignando rol: %w", err)
	}
	return tx.Commit()
}

// CountActiveSuperAdmins cuenta los super_admin activos sin contar a
// excludeUserID (vacío = contarlos todos).
func (r *SQLRepository) CountActiveSuperAdmins(ctx context.Context, excludeUserID string) (int, error) {
	var n int
	err := r.conn.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id
		WHERE ur.role_id = ? AND u.status = ? AND u.id <> ?`,
		RoleSuperAdmin, string(StatusActive), excludeUserID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("contando super_admin: %w", err)
	}
	return n, nil
}

// ListGroupMembers devuelve los miembros del grupo ordenados por nombre de
// usuario. ErrNotFound si el grupo no existe.
func (r *SQLRepository) ListGroupMembers(ctx context.Context, groupID string) ([]*User, error) {
	if _, err := r.GetGroupByID(ctx, groupID); err != nil {
		return nil, err
	}
	rows, err := r.conn.QueryContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.email, u.password_hash, u.status, u.quota_bytes, u.totp_secret, u.created_at, u.updated_at, u.last_login_at
		FROM users u JOIN user_groups ug ON ug.user_id = u.id
		WHERE ug.group_id = ? ORDER BY u.username`, groupID)
	if err != nil {
		return nil, fmt.Errorf("listando miembros del grupo: %w", err)
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// RemoveUserFromGroup saca a userID del grupo. ErrNotFound si no era
// miembro (o el grupo o el usuario no existen).
func (r *SQLRepository) RemoveUserFromGroup(ctx context.Context, userID, groupID string) error {
	res, err := r.conn.ExecContext(ctx,
		`DELETE FROM user_groups WHERE user_id = ? AND group_id = ?`, userID, groupID)
	if err != nil {
		return fmt.Errorf("quitando miembro del grupo: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// RenameGroup cambia el nombre del grupo. ErrAlreadyExists si el nombre ya
// lo usa otro grupo; ErrNotFound si el grupo no existe.
func (r *SQLRepository) RenameGroup(ctx context.Context, groupID, name string) error {
	res, err := r.conn.ExecContext(ctx,
		`UPDATE `+groupsTable(r.conn.Driver)+` SET name = ? WHERE id = ?`, name, groupID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("renombrando grupo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		// MySQL cuenta filas CAMBIADAS: el mismo nombre da 0 sin que falte.
		if _, err := r.GetGroupByID(ctx, groupID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteGroup borra el grupo. Las FK con ON DELETE CASCADE se llevan sus
// membresías (user_groups) y las comparticiones dirigidas a él (shares).
func (r *SQLRepository) DeleteGroup(ctx context.Context, groupID string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM `+groupsTable(r.conn.Driver)+` WHERE id = ?`, groupID)
	if err != nil {
		return fmt.Errorf("borrando grupo: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// groupsTable (ADR-031): "groups" a secas es palabra reservada en MySQL 8+
// (introducida para la unidad de ventana ROWS|RANGE|GROUPS, SQL:2016) --
// CREATE TABLE/INSERT/SELECT sin comillas contra ella falla con
// "ERROR 1064: ... syntax ... near 'groups'". Postgres no soporta comillas
// invertidas como delimitador de identificador (solo comillas dobles, y
// activar ANSI_QUOTES globalmente en MySQL para poder usar comillas dobles
// ahí tendría efectos colaterales sobre el resto de sentencias), así que
// la única forma portable es entrecomillar SOLO en la rama mysql, aquí y
// en ListGroups/GroupsForUser -- los 3 únicos sitios de todo el código Go
// que referencian esta tabla por nombre.
func groupsTable(driver string) string {
	if driver == "mysql" {
		return "`groups`"
	}
	return "groups"
}

func (r *SQLRepository) UpdateGroupQuota(ctx context.Context, groupID string, quota *int64) error {
	res, err := r.conn.ExecContext(ctx,
		`UPDATE `+groupsTable(r.conn.Driver)+` SET quota_bytes = ? WHERE id = ?`,
		nullInt64(quota), groupID)
	if err != nil {
		return fmt.Errorf("actualizando la cuota del grupo: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		// MySQL cuenta filas CAMBIADAS, no encontradas: repetir el mismo valor
		// da 0 sin que el grupo falte. Se distingue mirando si existe.
		if _, err := r.GetGroupByID(ctx, groupID); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLRepository) CreateGroup(ctx context.Context, g *Group) error {
	_, err := r.conn.ExecContext(ctx,
		`INSERT INTO `+groupsTable(r.conn.Driver)+` (id, name, quota_bytes, created_at) VALUES (?, ?, ?, ?)`,
		g.ID, g.Name, nullInt64(g.QuotaBytes), db.TimeToString(g.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("creando grupo: %w", err)
	}
	return nil
}

// GetGroupByID y GetGroupByName completan el mismo par que ya existe para
// User (GetUserByID/GetUserByUsername) -- hasta ahora Group solo tenía
// ListGroups, insuficiente para que el handler HTTP de "añadir miembro"
// (POST /groups/{id}/members) pueda devolver 404 real en vez de dejar que
// una violación de FK en AddUserToGroup se filtre como error 500 genérico.
func (r *SQLRepository) GetGroupByID(ctx context.Context, id string) (*Group, error) {
	row := r.conn.QueryRowContext(ctx, `SELECT id, name, quota_bytes, created_at FROM `+groupsTable(r.conn.Driver)+` WHERE id = ?`, id)
	return scanGroup(row)
}

func (r *SQLRepository) GetGroupByName(ctx context.Context, name string) (*Group, error) {
	row := r.conn.QueryRowContext(ctx, `SELECT id, name, quota_bytes, created_at FROM `+groupsTable(r.conn.Driver)+` WHERE name = ?`, name)
	return scanGroup(row)
}

func scanGroup(row *sql.Row) (*Group, error) {
	g := &Group{}
	var quota sql.NullInt64
	var createdAt string
	if err := row.Scan(&g.ID, &g.Name, &quota, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if quota.Valid {
		g.QuotaBytes = &quota.Int64
	}
	var err error
	g.CreatedAt, err = db.StringToTime(createdAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

func (r *SQLRepository) ListGroups(ctx context.Context) ([]*Group, error) {
	rows, err := r.conn.QueryContext(ctx, `SELECT id, name, quota_bytes, created_at FROM `+groupsTable(r.conn.Driver)+` ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listando grupos: %w", err)
	}
	defer rows.Close()

	var out []*Group
	for rows.Next() {
		g := &Group{}
		var quota sql.NullInt64
		var createdAt string
		if err := rows.Scan(&g.ID, &g.Name, &quota, &createdAt); err != nil {
			return nil, err
		}
		if quota.Valid {
			g.QuotaBytes = &quota.Int64
		}
		g.CreatedAt, err = db.StringToTime(createdAt)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *SQLRepository) AddUserToGroup(ctx context.Context, userID, groupID string) error {
	_, err := r.conn.ExecContext(ctx,
		`INSERT INTO user_groups (user_id, group_id) VALUES (?, ?)`, userID, groupID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return fmt.Errorf("añadiendo usuario a grupo: %w", err)
	}
	return nil
}

func (r *SQLRepository) GroupsForUser(ctx context.Context, userID string) ([]Group, error) {
	rows, err := r.conn.QueryContext(ctx, `
		SELECT g.id, g.name, g.quota_bytes, g.created_at FROM `+groupsTable(r.conn.Driver)+` g
		JOIN user_groups ug ON ug.group_id = g.id
		WHERE ug.user_id = ? ORDER BY g.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("consultando grupos del usuario: %w", err)
	}
	defer rows.Close()

	var out []Group
	for rows.Next() {
		var g Group
		var quota sql.NullInt64
		var createdAt string
		if err := rows.Scan(&g.ID, &g.Name, &quota, &createdAt); err != nil {
			return nil, err
		}
		if quota.Valid {
			g.QuotaBytes = &quota.Int64
		}
		g.CreatedAt, err = db.StringToTime(createdAt)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

const userSelectColumns = `SELECT id, username, display_name, email, password_hash, status, quota_bytes, totp_secret, created_at, updated_at, last_login_at FROM users`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row *sql.Row) (*User, error) {
	u, err := scanUserRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func scanUserRow(row rowScanner) (*User, error) {
	var (
		u                            User
		email, totp                  sql.NullString
		quota                        sql.NullInt64
		status, createdAt, updatedAt string
		lastLoginAt                  sql.NullString
	)
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &email, &u.PasswordHash, &status,
		&quota, &totp, &createdAt, &updatedAt, &lastLoginAt); err != nil {
		return nil, err
	}
	u.Status = Status(status)
	if email.Valid {
		u.Email = email.String
	}
	if totp.Valid {
		u.TOTPSecret = totp.String
	}
	if quota.Valid {
		u.QuotaBytes = &quota.Int64
	}
	var err error
	u.CreatedAt, err = db.StringToTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parseando created_at: %w", err)
	}
	u.UpdatedAt, err = db.StringToTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parseando updated_at: %w", err)
	}
	u.LastLoginAt, err = db.ParseNullableTime(lastLoginAt.String, lastLoginAt.Valid)
	if err != nil {
		return nil, fmt.Errorf("parseando last_login_at: %w", err)
	}
	return &u, nil
}

func rowsAffectedOrNotFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("comprobando filas afectadas: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// isUniqueViolation reconoce el mensaje de violación de restricción UNIQUE
// tanto de sqlite (modernc.org/sqlite) como de postgres (pgx) y mysql
// (go-sql-driver/mysql: "Duplicate entry ... for key ..."), evitando una
// dependencia directa en los tipos de error internos de cada driver.
func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "duplicate entry")
}
