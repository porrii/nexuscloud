//go:build !linux

package storage

import "os/exec"

// configureSysProcAttr no hace nada fuera de Linux: sin Setpgid ni
// cgroups, el timeout de exec.CommandContext solo mata el proceso
// principal (comportamiento por defecto, cmd.Process.Kill()), nunca un
// grupo de procesos completo -- asimetría real y documentada (§34
// Decisión 4), mismo criterio que docs/deployment.md ya aplica al
// hardening de systemd frente a Windows.
func configureSysProcAttr(cmd *exec.Cmd) {}
