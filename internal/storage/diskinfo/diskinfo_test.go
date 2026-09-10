package diskinfo

import (
	"context"
	"errors"
	"testing"
)

// TestEnumerateContract comprueba el contrato público en la plataforma
// actual: o bien devuelve unidades sin error, o bien ErrUnsupported. Nunca
// un error distinto en una máquina de CI sana.
func TestEnumerateContract(t *testing.T) {
	disks, err := Enumerate(context.Background())
	switch {
	case errors.Is(err, ErrUnsupported):
		if disks != nil {
			t.Errorf("con ErrUnsupported, disks debe ser nil, got %d", len(disks))
		}
	case err != nil:
		t.Fatalf("Enumerate devolvió un error inesperado: %v", err)
	default:
		for i, d := range disks {
			if d.MountPoint == "" {
				t.Errorf("disco %d sin MountPoint", i)
			}
			if d.TotalBytes == 0 {
				t.Errorf("disco %d (%s) con TotalBytes 0", i, d.MountPoint)
			}
			if d.FreeBytes > d.TotalBytes {
				t.Errorf("disco %d (%s): FreeBytes %d > TotalBytes %d", i, d.MountPoint, d.FreeBytes, d.TotalBytes)
			}
		}
	}
}

func TestEnumerateRespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Enumerate(ctx)
	// En plataformas soportadas debe propagar la cancelación; en las no
	// soportadas, ErrUnsupported es igualmente aceptable (no llega a
	// iterar).
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error inesperado con contexto cancelado: %v", err)
	}
}
