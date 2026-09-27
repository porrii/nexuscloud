package apiv1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// TestRecovererMiddlewareEvitaQueUnPanicTumbeElServidor confirma el
// mecanismo exacto que NewRouter usa (§34, ADR-041 Decisión 6, hallazgo
// CRÍTICO del pase de security-reviewer sobre el diseño de miniaturas):
// hasta esa fase ningún panic de Go se recuperaba en todo el proyecto --
// un panic de decodificación en cualquier handler (ahora que existen
// handlers que sí interpretan contenido de usuario) tumbaría el proceso
// entero para todos los inquilinos. No reconstruye el NewRouter real
// (pediría montar todas sus dependencias) -- confirma que
// middleware.Recoverer, en la misma posición (primero, antes que
// cualquier otro middleware), convierte un panic en 500 en vez de
// propagarse.
func TestRecovererMiddlewareEvitaQueUnPanicTumbeElServidor(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/paniquea", func(w http.ResponseWriter, r *http.Request) {
		panic("fallo simulado, p.ej. un decodificador de imagen ante bytes adversariales")
	})

	req := httptest.NewRequest(http.MethodGet, "/paniquea", nil)
	rw := httptest.NewRecorder()

	// Si middleware.Recoverer no estuviera aplicado, ServeHTTP propagaría
	// el panic y ESTE TEST (no solo el handler) terminaría en pánico --
	// la propia ejecución sin crashear ya es la mitad de la prueba.
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, esperado %d (recuperado, no propagado)", rw.Code, http.StatusInternalServerError)
	}
}
