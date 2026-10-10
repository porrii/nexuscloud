//go:build linux

package storage

import (
	"os/exec"
	"syscall"
)

// configureSysProcAttr aísla el grupo de procesos de ffmpeg/pdftoppm
// (§34 Decisión 4) para que el timeout mate también a cualquier hijo que
// lancen (Setpgid), no solo al proceso principal -- cmd.Cancel sustituye
// el comportamiento por defecto de exec.CommandContext (que solo mata
// cmd.Process) por un SIGKILL al grupo entero (PID negativo).
func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
