package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalFilesystemProvider implementa Provider sobre el filesystem local
// bajo root. Cada operación pasa por SafeJoin: aunque el llamador ya
// debería haber validado la ruta (FileService), este provider nunca confía
// ciegamente en el string que recibe (§168 Zero Trust, §194).
type LocalFilesystemProvider struct {
	root string
}

func NewLocalFilesystemProvider(root string) (*LocalFilesystemProvider, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolviendo raíz de almacenamiento: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("creando raíz de almacenamiento %s: %w", abs, err)
	}
	return &LocalFilesystemProvider{root: abs}, nil
}

func (p *LocalFilesystemProvider) Write(ctx context.Context, relPath string, r io.Reader) (int64, string, error) {
	full, err := SafeJoin(p.root, relPath)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return 0, "", fmt.Errorf("creando directorio destino: %w", err)
	}

	// Escritura atómica: fichero temporal en el mismo directorio + rename,
	// para que un fallo a mitad de subida nunca deje un archivo corrupto
	// visible en su ruta final (§94 Fail Safe, §95 Corrupción).
	tmp, err := os.CreateTemp(filepath.Dir(full), ".nexuscloud-upload-*")
	if err != nil {
		return 0, "", fmt.Errorf("creando fichero temporal: %w", err)
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		tmp.Close()
		if !success {
			os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), r)
	if err != nil {
		return 0, "", fmt.Errorf("escribiendo contenido: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return 0, "", fmt.Errorf("sincronizando fichero: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, "", fmt.Errorf("cerrando fichero temporal: %w", err)
	}
	if err := os.Rename(tmpPath, full); err != nil {
		return 0, "", fmt.Errorf("moviendo fichero a destino final: %w", err)
	}
	success = true
	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

func (p *LocalFilesystemProvider) Read(ctx context.Context, relPath string) (io.ReadCloser, error) {
	full, err := SafeJoin(p.root, relPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (p *LocalFilesystemProvider) Delete(ctx context.Context, relPath string) error {
	full, err := SafeJoin(p.root, relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (p *LocalFilesystemProvider) Exists(ctx context.Context, relPath string) (bool, error) {
	full, err := SafeJoin(p.root, relPath)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(full)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (p *LocalFilesystemProvider) MkdirAll(ctx context.Context, relPath string) error {
	full, err := SafeJoin(p.root, relPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(full, 0o750)
}

func (p *LocalFilesystemProvider) Move(ctx context.Context, fromRelPath, toRelPath string) error {
	fromFull, err := SafeJoin(p.root, fromRelPath)
	if err != nil {
		return err
	}
	toFull, err := SafeJoin(p.root, toRelPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(toFull), 0o750); err != nil {
		return fmt.Errorf("creando directorio destino: %w", err)
	}
	if err := os.Rename(fromFull, toFull); err != nil {
		return fmt.Errorf("moviendo %s a %s: %w", fromRelPath, toRelPath, err)
	}
	return nil
}
