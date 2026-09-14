#!/usr/bin/env bash
# Desinstala NexusCloud (instalación nativa systemd). Por seguridad NO borra
# los datos ni la configuración salvo que se pase --purge.
#
# Uso:
#   sudo ./uninstall.sh            # quita servicio y binario, conserva datos
#   sudo ./uninstall.sh --purge    # además borra datos, config y el usuario
set -euo pipefail

NX_PREFIX="${NX_PREFIX:-/usr/local/bin}"
NX_CONFIG_DIR="${NX_CONFIG_DIR:-/etc/nexuscloud}"
NX_DATA_DIR="${NX_DATA_DIR:-/var/lib/nexuscloud}"
NX_USER="${NX_USER:-nexuscloud}"

PURGE=no
[ "${1:-}" = "--purge" ] && PURGE=yes

die() { echo "uninstall.sh: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo)."

if command -v systemctl >/dev/null; then
    echo "==> Parando y deshabilitando el servicio"
    systemctl stop nexuscloud 2>/dev/null || true
    systemctl disable nexuscloud 2>/dev/null || true
    rm -f /etc/systemd/system/nexuscloud.service
    systemctl daemon-reload
fi

echo "==> Quitando el binario"
rm -f "${NX_PREFIX}/nexuscloud" "${NX_PREFIX}"/nexuscloud.bak-*

if [ "$PURGE" = yes ]; then
    echo "==> --purge: borrando datos, configuración y usuario"
    rm -rf "$NX_DATA_DIR" "$NX_CONFIG_DIR"
    if id "$NX_USER" >/dev/null 2>&1; then
        userdel "$NX_USER" 2>/dev/null || true
    fi
    echo "    NexusCloud eliminado por completo."
else
    cat <<EOF
    Servicio y binario eliminados.
    Se CONSERVAN los datos y la configuración:
      ${NX_DATA_DIR}
      ${NX_CONFIG_DIR}
      usuario del sistema '${NX_USER}'
    Para borrarlos también:  sudo ./uninstall.sh --purge
EOF
fi
