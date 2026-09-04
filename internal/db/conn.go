package db

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Conn envuelve un *sql.DB reescribiendo automáticamente los placeholders
// "?" al estilo que exige cada driver (ver Rebind). Los repositorios de
// cada dominio reciben un *Conn en vez de un *sql.DB crudo: así cada
// consulta se escribe una única vez y funciona igual en sqlite y postgres
// sin necesidad de un ORM completo (§8, §105).
type Conn struct {
	*sql.DB
	Driver string
}

func Wrap(driver string, conn *sql.DB) *Conn {
	return &Conn{DB: conn, Driver: driver}
}

func (c *Conn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, Rebind(c.Driver, query), args...)
}

func (c *Conn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.DB.QueryContext(ctx, Rebind(c.Driver, query), args...)
}

func (c *Conn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, Rebind(c.Driver, query), args...)
}

// Rebind reescribe los placeholders "?" de una consulta al estilo que
// exige el driver de destino: sqlite entiende "?" nativamente, postgres
// (pgx) exige "$1", "$2", ...
func Rebind(driver string, query string) string {
	if driver != "postgres" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
