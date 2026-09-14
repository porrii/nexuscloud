#!/usr/bin/env bash
# Actualiza una instalación nativa de NexusCloud (systemd) a un binario
# nuevo, sin destruir datos (NEXUSCLOUD.md §59): para el servicio, sustituye
# el binario (guardando el anterior), aplica migraciones y vuelve a
# arrancar.
#
# Uso:  sudo ./update.sh <ruta-al-binario-nuevo>
set -euo pipefail

NX_PREFIX="${NX_PREFIX:-/usr/local/bin}"
NX_CONFIG_DIR="${NX_CONFIG_DIR:-/etc/nexuscloud}"
NX_USER="${NX_USER:-nexuscloud}"
CONFIG_FILE="${NX_CONFIG_DIR}/config.yaml"
TARGET="${NX_PREFIX}/nexuscloud"

die() { echo "update.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo)."
[ $# -ge 1 ] || die "uso: sudo ./update.sh <ruta-al-binario-nuevo>"
BIN_SRC="$1"
[ -x "$BIN_SRC" ] || die "no encuentro el binario ejecutable en: $BIN_SRC"
[ -f "$CONFIG_FILE" ] || die "no encuentro ${CONFIG_FILE}; ¿está NexusCloud instalado?"

OLD_VER="$("$TARGET" version 2>/dev/null || echo desconocida)"
NEW_VER="$("$BIN_SRC" version 2>/dev/null || echo desconocida)"
echo "==> ${OLD_VER}  ->  ${NEW_VER}"

WAS_ACTIVE=no
if systemctl is-active --quiet nexuscloud; then
    WAS_ACTIVE=yes
    echo "==> Parando el servicio"
    systemctl stop nexuscloud
fi

BACKUP="${TARGET}.bak-$(date +%Y%m%d%H%M%S)"
echo "==> Copia de seguridad del binario -> ${BACKUP}"
cp -p "$TARGET" "$BACKUP"

echo "==> Instalando el binario nuevo"
install -m 0755 "$BIN_SRC" "$TARGET"

echo "==> Migraciones de base de datos"
if ! sudo -u "$NX_USER" "$TARGET" --config "$CONFIG_FILE" migrate up; then
    echo "!! migrate up falló: restaurando el binario anterior" >&2
    install -m 0755 "$BACKUP" "$TARGET"
    [ "$WAS_ACTIVE" = yes ] && systemctl start nexuscloud
    exit 1
fi

if [ "$WAS_ACTIVE" = yes ]; then
    echo "==> Arrancando el servicio"
    systemctl start nexuscloud
    if systemctl is-active --quiet nexuscloud; then
        echo "    OK"
    else
        die "el servicio no arrancó; revisa 'journalctl -u nexuscloud'"
    fi
else
    echo "==> El servicio estaba parado; no se arranca."
fi
echo "Actualización completada."
