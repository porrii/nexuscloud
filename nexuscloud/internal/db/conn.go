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

// BeginTx abre una transacción real (ADR-030: mover una carpeta con
// descendientes es la primera operación del proyecto que toca varias
// filas/tablas a la vez -- todo lo anterior era una única sentencia, por
// eso nunca había hecho falta esto). El *Tx devuelto reescribe "?" igual
// que Conn, para que las mismas consultas parametrizadas sigan
// funcionando idénticas en sqlite y postgres dentro de la transacción.
// Patrón de uso: `tx, err := conn.BeginTx(ctx); defer tx.Rollback()` justo
// después, y `tx.Commit()` explícito en el camino feliz -- un Rollback
// tras un Commit ya hecho es un no-op seguro (comportamiento estándar de
// database/sql).
func (c *Conn) BeginTx(ctx context.Context) (*Tx, error) {
	sqlTx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: sqlTx, Driver: c.Driver}, nil
}

// Tx es el equivalente de Conn para una transacción en curso -- ver
// BeginTx.
type Tx struct {
	*sql.Tx
	Driver string
}

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, Rebind(t.Driver, query), args...)
}

func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, Rebind(t.Driver, query), args...)
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, Rebind(t.Driver, query), args...)
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
