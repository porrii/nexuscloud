package webdav

import (
	"errors"
	"time"

	xwebdav "golang.org/x/net/webdav"
)

// lockSystem envuelve la MemLS de x/net para corregir cómo valida la cabecera
// If en las peticiones que afectan a dos recursos (MOVE).
//
// x/net exige que las condiciones de If cubran a la vez el origen y el
// destino. RFC 4918 §7.5 solo pide token para los recursos que están
// bloqueados: un cliente que bloquea un archivo y luego lo renombra (davfs2 y,
// en general, los que bloquean antes de escribir) envía únicamente el token
// del origen, y con x/net recibe 412 Precondition Failed aunque el destino
// esté libre. Es un defecto conocido de x/net (canario en locks_test.go).
type lockSystem struct{ xwebdav.LockSystem }

// newLockSystem devuelve el LockSystem de un usuario: una MemLS propia (los
// locks son en memoria y se pierden al reiniciar, límite documentado en
// ADR-034) con la corrección de Confirm.
func newLockSystem() xwebdav.LockSystem { return &lockSystem{xwebdav.NewMemLS()} }

// Confirm actúa como el de x/net salvo cuando las condiciones no cubren los
// dos recursos a la vez. Entonces se comprueba cada uno por separado: al menos
// uno tiene que estar cubierto por un token presentado (si no, las condiciones
// de If no se cumplen y 412 es la respuesta correcta) y el otro tiene que estar
// libre de bloqueos ajenos.
func (l *lockSystem) Confirm(now time.Time, name0, name1 string, conditions ...xwebdav.Condition) (func(), error) {
	release, err := l.LockSystem.Confirm(now, name0, name1, conditions...)
	if !errors.Is(err, xwebdav.ErrConfirmationFailed) || name0 == "" || name1 == "" {
		return release, err
	}

	if r0, err0 := l.LockSystem.Confirm(now, name0, "", conditions...); err0 == nil {
		return l.holdFree(now, name1, r0)
	}
	if r1, err1 := l.LockSystem.Confirm(now, "", name1, conditions...); err1 == nil {
		return l.holdFree(now, name0, r1)
	}
	return nil, xwebdav.ErrConfirmationFailed
}

// holdFree retiene name mientras dura la operación con un bloqueo temporal,
// igual que hace x/net con los recursos de una petición sin If: si otro
// bloqueo ya lo cubre, falla y libera lo que había retenido.
func (l *lockSystem) holdFree(now time.Time, name string, releaseCovered func()) (func(), error) {
	token, err := l.LockSystem.Create(now, xwebdav.LockDetails{Root: name, Duration: -1, ZeroDepth: true})
	if err != nil {
		releaseCovered()
		if errors.Is(err, xwebdav.ErrLocked) {
			// El handler de x/net solo entiende ErrConfirmationFailed aquí; con
			// otro error respondería 500.
			return nil, xwebdav.ErrConfirmationFailed
		}
		return nil, err
	}
	return func() {
		_ = l.LockSystem.Unlock(now, token)
		releaseCovered()
	}, nil
}
