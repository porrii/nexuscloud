#!/usr/bin/env bash
# Añade la interfaz web a una instalación nativa de NexusCloud (systemd) YA
# HECHA sin ella (install.sh sin --web, el caso por defecto). En un solo
# paso: Node.js si falta -> compila la web -> recompila el binario con la
# web real embebida -> activa web.enabled en el config.yaml real -> deja
# que update.sh (parar/backup/instalar/migrar/reiniciar) haga el resto.
#
# La dirección inversa ("quitar la web") no necesita un script propio: ya
# funciona con lo que existe -- vuelve a compilar con la raíz ./install.sh
# normal (sin --web) y pasa ese binario a update.sh, exactamente igual que
# aquí pero sin el paso de Node/npm.
#
# Uso:  sudo ./enable-web.sh
set -euo pipefail

NX_CONFIG_DIR="${NX_CONFIG_DIR:-/etc/nexuscloud}"
CONFIG_FILE="${NX_CONFIG_DIR}/config.yaml"
MIN_NODE_MAJOR="20"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

die() { echo "enable-web.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo)."
[ -f "$CONFIG_FILE" ] || die "no encuentro ${CONFIG_FILE}; ¿está NexusCloud instalado? (usa NX_CONFIG_DIR si lo moviste)"
[ -d "${CODE_DIR}/web" ] || die "no encuentro ${CODE_DIR}/web -- ¿este script sigue dentro de nexuscloud/deploy/scripts/ del repo clonado?"

echo "==> Node.js (para compilar la interfaz web)"
need_node_install=1
if command -v node >/dev/null 2>&1; then
  current_node="$(node --version | sed -n 's/^v\([0-9]*\).*/\1/p')"
  if [ -n "$current_node" ] && [ "$current_node" -ge "$MIN_NODE_MAJOR" ]; then
    need_node_install=0
    echo "    Node.js $(node --version) ya instalado (>= v${MIN_NODE_MAJOR}), no hace falta reinstalar"
  fi
fi

if [ "$need_node_install" -eq 1 ]; then
  echo "    Instalando Node.js (no encontrado, o versión anterior a v${MIN_NODE_MAJOR})"
  case "$(uname -m)" in
    x86_64)  node_arch="x64" ;;
    aarch64) node_arch="arm64" ;;
    *) die "arquitectura no reconocida para Node.js: $(uname -m) -- instala Node.js manualmente desde https://nodejs.org/ y vuelve a lanzar este script." ;;
  esac
  node_version="$(curl -fsSL https://nodejs.org/dist/index.tab | awk -F'\t' '$10 != "-" && $10 != "lts" {print $1}' | head -n1)"
  [ -n "$node_version" ] || die "no pude determinar la última versión LTS de Node.js desde nodejs.org -- instálalo manualmente y vuelve a lanzar este script."
  tarball="node-${node_version}-linux-${node_arch}.tar.gz"
  tmp_tarball="$(mktemp -t nexuscloud-node-XXXXXX.tar.gz)"
  trap 'rm -f "$tmp_tarball"' EXIT
  echo "    descargando ${tarball} (LTS)..."
  curl -fsSL "https://nodejs.org/dist/${node_version}/${tarball}" -o "$tmp_tarball"
  rm -rf /usr/local/lib/nodejs-nexuscloud
  mkdir -p /usr/local/lib/nodejs-nexuscloud
  tar -C /usr/local/lib/nodejs-nexuscloud --strip-components=1 -xzf "$tmp_tarball"
  rm -f "$tmp_tarball"
  trap - EXIT
  export PATH="/usr/local/lib/nodejs-nexuscloud/bin:${PATH}"
  echo "    instalado en /usr/local/lib/nodejs-nexuscloud"
fi

if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
  # install.sh, si tuvo que instalar Go, lo deja en /usr/local/go sin
  # persistir el PATH (solo lo avisa) -- este script suele correr en una
  # sesión de shell NUEVA, días o semanas después, así que hay que mirar
  # aquí también antes de darlo por ausente.
  export PATH="/usr/local/go/bin:${PATH}"
fi
command -v go >/dev/null 2>&1 || die "no encuentro 'go' en el PATH (ni en /usr/local/go/bin) -- instálalo o añádelo al PATH y vuelve a lanzar este script."

echo "==> Compilando la interfaz web (${CODE_DIR}/web)"
(
  cd "${CODE_DIR}/web"
  npm ci --no-audit --no-fund
  npm run build
)

echo "==> Compilando nexuscloud con la web embebida"
build_dir="$(mktemp -d -t nexuscloud-build-XXXXXX)"
trap 'rm -rf "$build_dir"' EXIT
(
  cd "$CODE_DIR"
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "${build_dir}/nexuscloud" ./cmd/nexuscloud
)
echo "    binario listo"

echo "==> Activando web.enabled en ${CONFIG_FILE}"
if grep -q '^web:' "$CONFIG_FILE"; then
  sed -i '/^web:/{n;s/enabled: false/enabled: true/}' "$CONFIG_FILE"
else
  # Un config.yaml de antes de que "web" existiera como sección (no
  # debería pasar hoy, config init siempre la incluye, pero por si viene
  # de una instalación muy antigua editada a mano).
  printf '\nweb:\n  enabled: true\n' >> "$CONFIG_FILE"
fi

echo "==> Instalando el binario nuevo (parar/backup/migrar/reiniciar vía update.sh)"
"${SCRIPT_DIR}/update.sh" "${build_dir}/nexuscloud"

echo "Interfaz web activada."
