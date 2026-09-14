#!/usr/bin/env bash
# Instala NexusCloud en Linux: compila desde código fuente (instalando Go
# si hace falta) y lo registra como servicio con systemd. No usa Docker --
# para ese camino, usa `docker compose up -d` dentro de nexuscloud/ en su
# lugar (ver nexuscloud/docker-compose.yml).
#
# Pensado para un servidor SIN entorno gráfico: todo aquí es línea de
# comandos (curl/tar para Go, go build, useradd, systemctl) -- nada asume
# una sesión de escritorio.
#
# Uso:
#   sudo ./install.sh                    # compila desde código fuente
#   sudo ./install.sh /ruta/al/binario   # usa un binario ya compilado
#                                         # (descargado o cruzado a mano),
#                                         # se salta Go y la compilación
#
# Variables de entorno (todas opcionales, valores por defecto razonables):
#   NX_PREFIX      (por defecto /usr/local/bin)   -- dónde va el binario
#   NX_CONFIG_DIR  (por defecto /etc/nexuscloud)
#   NX_DATA_DIR    (por defecto /var/lib/nexuscloud)
#   NX_USER        (por defecto nexuscloud)       -- usuario de servicio
# Para fijarlas, pásalas junto a sudo en la misma línea, p.ej.:
#   sudo NX_DATA_DIR=/mnt/datos ./install.sh
#
# Para actualizar o desinstalar más adelante, usa
# nexuscloud/deploy/scripts/update.sh / uninstall.sh (mismas variables).
set -euo pipefail

NX_PREFIX="${NX_PREFIX:-/usr/local/bin}"
NX_CONFIG_DIR="${NX_CONFIG_DIR:-/etc/nexuscloud}"
NX_DATA_DIR="${NX_DATA_DIR:-/var/lib/nexuscloud}"
NX_USER="${NX_USER:-nexuscloud}"
CONFIG_FILE="${NX_CONFIG_DIR}/config.yaml"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="${SCRIPT_DIR}/nexuscloud"
UNIT_SRC="${CODE_DIR}/deploy/systemd/nexuscloud.service"
MIN_GO_VERSION="1.25"

die() { echo "install.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo ./install.sh)."
[ -d "$CODE_DIR" ] || die "no encuentro la carpeta nexuscloud/ junto a este script -- ¿lo estás ejecutando desde la raíz del repo clonado?"
command -v systemctl >/dev/null || die "systemd (systemctl) no disponible en este sistema; ver nexuscloud/docs/deployment.md para otros métodos (Docker)."
[ -f "$UNIT_SRC" ] || die "no encuentro la unit de systemd en: $UNIT_SRC"

# Con un binario ya dado como argumento, nos saltamos Go y la compilación
# por completo -- BIN_SRC queda fijado aquí y el bloque de abajo no se
# ejecuta (ver el "if [ -z ... ]" que lo envuelve).
BIN_SRC="${1:-}"
if [ -n "$BIN_SRC" ]; then
  [ -x "$BIN_SRC" ] || die "no encuentro un binario ejecutable en: $BIN_SRC"
fi

# ---- 1. Go: usa el que ya haya si cumple la versión mínima, si no lo
# instala desde el tarball oficial de go.dev (sin pasar por el gestor de
# paquetes de la distro, cuya versión empaquetada suele ir muy por detrás)
# -- todo este bloque (Go + compilar) se salta si ya se dio un binario.
version_ge() {
  # true si $1 >= $2, comparando como versiones "X.Y"(.Z)
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

if [ -z "$BIN_SRC" ]; then

need_go_install=1
if command -v go >/dev/null 2>&1; then
  current="$(go version | sed -n 's/^go version go\([0-9]*\.[0-9]*\).*/\1/p')"
  if [ -n "$current" ] && version_ge "$current" "$MIN_GO_VERSION"; then
    need_go_install=0
    echo "==> Go ${current} ya instalado (>= ${MIN_GO_VERSION}), no hace falta reinstalar"
  fi
fi

if [ "$need_go_install" -eq 1 ]; then
  echo "==> Instalando Go (no encontrado, o versión anterior a ${MIN_GO_VERSION})"
  case "$(uname -m)" in
    x86_64)  go_arch="amd64" ;;
    aarch64) go_arch="arm64" ;;
    armv7l)  go_arch="armv6l" ;;
    *) die "arquitectura no reconocida: $(uname -m) -- instala Go manualmente desde https://go.dev/dl/ y vuelve a lanzar este script." ;;
  esac
  go_version="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)"
  tarball="${go_version}.linux-${go_arch}.tar.gz"
  tmp_tarball="$(mktemp -t nexuscloud-go-XXXXXX.tar.gz)"
  trap 'rm -f "$tmp_tarball"' EXIT
  echo "    descargando ${tarball}..."
  curl -fsSL "https://go.dev/dl/${tarball}" -o "$tmp_tarball"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp_tarball"
  rm -f "$tmp_tarball"
  trap - EXIT
  export PATH="/usr/local/go/bin:${PATH}"
  echo "    instalado en /usr/local/go -- añade /usr/local/go/bin a tu PATH permanentemente"
  echo "    (p.ej. echo 'export PATH=\$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh) si vas a compilar aquí más veces"
fi

# ---- 2. Compilar el binario (mismos flags que nexuscloud/Dockerfile, sin
# CGO -- estático, no necesita glibc/musl en runtime) ---------------------
echo "==> Compilando nexuscloud (esto puede tardar la primera vez)"
build_dir="$(mktemp -d -t nexuscloud-build-XXXXXX)"
trap 'rm -rf "$build_dir"' EXIT
(
  cd "$CODE_DIR"
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "${build_dir}/nexuscloud" ./cmd/nexuscloud
)
BIN_SRC="${build_dir}/nexuscloud"
echo "    binario listo"

fi # fin del bloque "solo si no se dio ya un binario por argumento"

# La interfaz web (nexuscloud/web/) NO se compila aquí a propósito: exige
# Node.js además de Go, y viene desactivada por defecto (secure-by-default)
# -- si la quieres, sigue el paso manual del README tras esta instalación.

# ---- 3. Instalar como servicio (antes delegado en un
# deploy/scripts/install.sh separado -- unificado aquí en un único script,
# ya que el otro solo tenía sentido para quien partiera de un binario ya
# compilado, y ese caso ahora también lo cubre este mismo script) --------
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

Actualizar más adelante:  sudo nexuscloud/deploy/scripts/update.sh <nuevo-binario>
Desinstalar:              sudo nexuscloud/deploy/scripts/uninstall.sh   (añade --purge para borrar datos)
EOF
