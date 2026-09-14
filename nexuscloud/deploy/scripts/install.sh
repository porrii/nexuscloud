#!/usr/bin/env bash
# Instalación nativa de NexusCloud en Linux con systemd. No usa Docker.
#
# Uso:
#   sudo ./install.sh [ruta-al-binario-nexuscloud]
#
# Si no se pasa la ruta, se busca ./nexuscloud junto a este script.
# Variables de entorno para ajustar rutas (todas con valores por defecto
# razonables; la configuración de NexusCloud es independiente del método
# de despliegue):
#   NX_PREFIX      (por defecto /usr/local/bin)   -- dónde va el binario
#   NX_CONFIG_DIR  (por defecto /etc/nexuscloud)
#   NX_DATA_DIR    (por defecto /var/lib/nexuscloud)
#   NX_USER        (por defecto nexuscloud)       -- usuario de servicio
set -euo pipefail

NX_PREFIX="${NX_PREFIX:-/usr/local/bin}"
NX_CONFIG_DIR="${NX_CONFIG_DIR:-/etc/nexuscloud}"
NX_DATA_DIR="${NX_DATA_DIR:-/var/lib/nexuscloud}"
NX_USER="${NX_USER:-nexuscloud}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="${1:-${SCRIPT_DIR}/nexuscloud}"
UNIT_SRC="${SCRIPT_DIR}/../systemd/nexuscloud.service"
CONFIG_FILE="${NX_CONFIG_DIR}/config.yaml"

die() { echo "install.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo)."
[ -x "$BIN_SRC" ] || die "no encuentro el binario ejecutable en: $BIN_SRC"
command -v systemctl >/dev/null || die "systemd (systemctl) no disponible; ver docs/deployment.md para otros métodos."
[ -f "$UNIT_SRC" ] || die "no encuentro la unit de systemd en: $UNIT_SRC"

echo "==> Usuario de servicio '${NX_USER}'"
if ! id "$NX_USER" >/dev/null 2>&1; then
    useradd --system --home-dir "$NX_DATA_DIR" --shell /usr/sbin/nologin "$NX_USER"
    echo "    creado"
else
    echo "    ya existe"
fi

echo "==> Binario -> ${NX_PREFIX}/nexuscloud"
install -m 0755 "$BIN_SRC" "${NX_PREFIX}/nexuscloud"

echo "==> Directorios"
install -d -m 0750 -o "$NX_USER" -g "$NX_USER" "$NX_DATA_DIR"
install -d -m 0750 -o "$NX_USER" -g "$NX_USER" "$NX_CONFIG_DIR"

echo "==> Configuración"
if [ -f "$CONFIG_FILE" ]; then
    echo "    ${CONFIG_FILE} ya existe, no se toca"
else
    "${NX_PREFIX}/nexuscloud" config init --out "$CONFIG_FILE"
    chown "$NX_USER:$NX_USER" "$CONFIG_FILE"
    chmod 0640 "$CONFIG_FILE"
    echo "    revisa ${CONFIG_FILE} antes de arrancar (docs/security.md)"
fi

echo "==> Migraciones de base de datos"
sudo -u "$NX_USER" "${NX_PREFIX}/nexuscloud" --config "$CONFIG_FILE" migrate up

echo "==> Servicio systemd"
install -m 0644 "$UNIT_SRC" /etc/systemd/system/nexuscloud.service
if [ "$NX_DATA_DIR" != "/var/lib/nexuscloud" ]; then
    sed -i "s#ReadWritePaths=/var/lib/nexuscloud#ReadWritePaths=${NX_DATA_DIR}#" /etc/systemd/system/nexuscloud.service
fi
sed -i "s#ExecStart=/usr/local/bin/nexuscloud start --config /etc/nexuscloud/config.yaml#ExecStart=${NX_PREFIX}/nexuscloud start --config ${CONFIG_FILE}#" /etc/systemd/system/nexuscloud.service
systemctl daemon-reload
systemctl enable nexuscloud >/dev/null

cat <<EOF

NexusCloud instalado. El servicio NO se ha arrancado todavía.
  1. Revisa   ${CONFIG_FILE}
  2. Crea un admin:  sudo -u ${NX_USER} ${NX_PREFIX}/nexuscloud --config ${CONFIG_FILE} admin create-user
  3. Arranca:  sudo systemctl start nexuscloud
  4. Estado:   systemctl status nexuscloud

Actualizar más adelante:  sudo ./update.sh <nuevo-binario>
Desinstalar:              sudo ./uninstall.sh   (añade --purge para borrar datos)
EOF
