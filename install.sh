#!/usr/bin/env bash
# Instala NexusCloud en Linux: compila desde código fuente (instalando Go
# si hace falta) y registra el servicio con systemd. No usa Docker -- para
# ese camino, usa `docker compose up -d` dentro de nexuscloud/ en su lugar
# (ver nexuscloud/docker-compose.yml).
#
# Pensado para un servidor SIN entorno gráfico: todo aquí es línea de
# comandos (curl/tar para Go, go build, systemctl) -- nada asume una
# sesión de escritorio.
#
# Uso:
#   sudo ./install.sh
#
# Variables de entorno (todas opcionales, mismos valores por defecto que
# nexuscloud/deploy/scripts/install.sh, que es quien las usa de verdad):
#   NX_PREFIX      (por defecto /usr/local/bin)
#   NX_CONFIG_DIR  (por defecto /etc/nexuscloud)
#   NX_DATA_DIR    (por defecto /var/lib/nexuscloud)
#   NX_USER        (por defecto nexuscloud)
# Para fijarlas, pásalas junto a sudo en la misma línea, p.ej.:
#   sudo NX_DATA_DIR=/mnt/datos ./install.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="${SCRIPT_DIR}/nexuscloud"
MIN_GO_VERSION="1.25"

die() { echo "install.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo ./install.sh) -- compila el binario e instala el servicio systemd."
[ -d "$CODE_DIR" ] || die "no encuentro la carpeta nexuscloud/ junto a este script -- ¿lo estás ejecutando desde la raíz del repo clonado?"
command -v systemctl >/dev/null || die "systemd (systemctl) no disponible en este sistema; ver nexuscloud/docs/deployment.md para otros métodos (Docker)."

# ---- Go: usa el que ya haya si cumple la versión mínima, si no lo instala
# desde el tarball oficial de go.dev (sin pasar por el gestor de paquetes
# de la distro, cuya versión empaquetada suele ir muy por detrás) --------
version_ge() {
  # true si $1 >= $2, comparando como versiones "X.Y"(.Z)
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

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

# ---- Compilar el binario (mismos flags que nexuscloud/Dockerfile, sin
# CGO -- estático, no necesita glibc/musl en runtime) --------------------
echo "==> Compilando nexuscloud (esto puede tardar la primera vez)"
build_dir="$(mktemp -d -t nexuscloud-build-XXXXXX)"
trap 'rm -rf "$build_dir"' EXIT
(
  cd "$CODE_DIR"
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "${build_dir}/nexuscloud" ./cmd/nexuscloud
)
echo "    binario listo"

# La interfaz web (nexuscloud/web/) NO se compila aquí a propósito: exige
# Node.js además de Go, y viene desactivada por defecto (secure-by-default)
# -- si la quieres, sigue el paso manual del README tras esta instalación.

# ---- Delega TODO lo demás (usuario de servicio, directorios, config,
# migraciones, unit de systemd) en el script ya existente y probado ------
# Sin `exec`: `exec` sustituiría este proceso por el nuevo script y se
# saltaría el `trap ... EXIT` de arriba que limpia $build_dir -- una
# llamada normal deja que ese trap se dispare después, al terminar.
echo "==> Instalando el servicio"
"${CODE_DIR}/deploy/scripts/install.sh" "${build_dir}/nexuscloud"
