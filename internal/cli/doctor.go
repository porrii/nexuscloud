package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/porrii/nexuscloud/internal/config"
	"github.com/porrii/nexuscloud/internal/db"
)

type checkResult struct {
	Name   string
	Status string // PASS | WARNING | FAIL
	Detail string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnostica el estado de esta instalación (§99)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				printDoctorResults(cmd, []checkResult{{"Configuration", "FAIL", err.Error()}})
				return fmt.Errorf("diagnóstico con fallos críticos")
			}

			results := []checkResult{
				{"Configuration", "PASS", fmt.Sprintf("configVersion=%d", cfg.ConfigVersion)},
				checkDatabase(cfg),
				checkStorage(cfg),
				checkPort(cfg),
				checkTLS(cfg),
				checkWebPublic(cfg),
				checkPublicRegistration(cfg),
			}

			printDoctorResults(cmd, results)

			for _, r := range results {
				if r.Status == "FAIL" {
					return fmt.Errorf("diagnóstico con fallos críticos")
				}
			}
			return nil
		},
	}
}

func checkDatabase(cfg *config.Config) checkResult {
	sqlDB, err := db.Open(cfg)
	if err != nil {
		return checkResult{"Database", "FAIL", err.Error()}
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		return checkResult{"Database", "FAIL", "no se pudo conectar: " + err.Error()}
	}
	version, dirty, err := db.Status(cfg, sqlDB)
	if err != nil {
		return checkResult{"Database", "WARNING", "conectado pero no se pudo leer el estado de migraciones: " + err.Error()}
	}
	if dirty {
		return checkResult{"Database", "FAIL", fmt.Sprintf("esquema en estado dirty en la versión %d (una migración falló a mitad)", version)}
	}
	if version == 0 {
		return checkResult{"Database", "WARNING", "sin migrar todavía; ejecuta 'nexuscloud migrate up'"}
	}
	return checkResult{"Database", "PASS", fmt.Sprintf("%s, esquema v%d", cfg.Database.Driver, version)}
}

func checkStorage(cfg *config.Config) checkResult {
	if err := cfg.EnsureDataDirs(); err != nil {
		return checkResult{"Storage", "FAIL", err.Error()}
	}
	testFile := filepath.Join(cfg.DefaultStorageDir(), ".nexuscloud-doctor-write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return checkResult{"Storage", "FAIL", "no se puede escribir en " + cfg.DefaultStorageDir() + ": " + err.Error()}
	}
	_ = os.Remove(testFile)
	return checkResult{"Storage", "PASS", cfg.DefaultStorageDir()}
}

func checkPort(cfg *config.Config) checkResult {
	addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return checkResult{"Port", "WARNING", fmt.Sprintf("%s no disponible ahora mismo (¿ya hay una instancia corriendo?): %v", addr, err)}
	}
	_ = ln.Close()
	return checkResult{"Port", "PASS", addr}
}

func checkTLS(cfg *config.Config) checkResult {
	if cfg.Server.TLSCertFile == "" {
		return checkResult{"HTTPS", "WARNING", "TLS no configurado; aceptable en LAN, revisar antes de exponer a Internet (§67)"}
	}
	if _, err := os.Stat(cfg.Server.TLSCertFile); err != nil {
		return checkResult{"HTTPS", "FAIL", "tlsCertFile no accesible: " + err.Error()}
	}
	if _, err := os.Stat(cfg.Server.TLSKeyFile); err != nil {
		return checkResult{"HTTPS", "FAIL", "tlsKeyFile no accesible: " + err.Error()}
	}
	return checkResult{"HTTPS", "PASS", "TLS configurado"}
}

func checkWebPublic(cfg *config.Config) checkResult {
	if cfg.Web.Enabled {
		return checkResult{"Web pública", "WARNING", "interfaz web activada — asegúrate de que es intencional"}
	}
	return checkResult{"Web pública", "PASS", "desactivada (secure by default, §3/§47)"}
}

func checkPublicRegistration(cfg *config.Config) checkResult {
	if cfg.Security.PublicRegistrationEnabled {
		return checkResult{"Public registration", "WARNING", "registro público activado — no recomendado (§21)"}
	}
	return checkResult{"Public registration", "PASS", "desactivado"}
}

func printDoctorResults(cmd *cobra.Command, results []checkResult) {
	for _, r := range results {
		fmt.Fprintf(cmd.OutOrStdout(), "%-9s %-20s %s\n", r.Status, r.Name, r.Detail)
	}
}
