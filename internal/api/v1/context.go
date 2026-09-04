// Package apiv1 implementa la API REST versionada bajo /api/v1 (§42).
package apiv1

import (
	"context"

	"github.com/porrii/nexuscloud/internal/auth"
	"github.com/porrii/nexuscloud/internal/users"
)

type contextKey int

const (
	ctxUser contextKey = iota
	ctxSession
)

func UserFromContext(ctx context.Context) (*users.User, bool) {
	u, ok := ctx.Value(ctxUser).(*users.User)
	return u, ok
}

func SessionFromContext(ctx context.Context) (*auth.Session, bool) {
	s, ok := ctx.Value(ctxSession).(*auth.Session)
	return s, ok
}
