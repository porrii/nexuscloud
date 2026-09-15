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
#   sudo ./install.sh                    # compila desde código fuente, sin la interfaz web
#   sudo ./install.sh --web              # igual, pero compila e incluye también la interfaz web
#   sudo ./install.sh /ruta/al/binario   # usa un binario ya compilado
#                                         # (descargado o cruzado a mano),
#                                         # se salta Go y la compilación
#                                         # (--web no tiene efecto en este caso: ver más abajo)
#
# --web es opt-in a propósito (secure/lean by default, igual criterio que
# el resto del proyecto): sin él, el binario embebe solo un placeholder
# vacío (0 bytes de JS/CSS de la web), y `web.enabled` queda en `false` en
# el config.yaml generado. Añadir la web más adelante a una instalación ya
# hecha, sin reinstalar desde cero: nexuscloud/deploy/scripts/enable-web.sh
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
MIN_NODE_MAJOR="20"

die() { echo "install.sh: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "hay que ejecutarlo como root (sudo ./install.sh)."
[ -d "$CODE_DIR" ] || die "no encuentro la carpeta nexuscloud/ junto a este script -- ¿lo estás ejecutando desde la raíz del repo clonado?"
command -v systemctl >/dev/null || die "systemd (systemctl) no disponible en este sistema; ver nexuscloud/docs/deployment.md para otros métodos (Docker)."
[ -f "$UNIT_SRC" ] || die "no encuentro la unit de systemd en: $UNIT_SRC"

# Bucle de argumentos: --web es una bandera (en cualquier posición), y como
# mucho un positional (BIN_SRC, ruta a un binario ya compilado) -- antes
# solo existía el positional, así que ya no basta con "${1:-}".
WITH_WEB=0
BIN_SRC=""
for arg in "$@"; do
  case "$arg" in
    --web)
      WITH_WEB=1
      ;;
    -*)
      die "opción no reconocida: $arg (uso: ./install.sh [--web] [/ruta/al/binario])"
      ;;
    *)
      [ -z "$BIN_SRC" ] || die "solo se admite una ruta de binario (ya se dio: $BIN_SRC)"
      BIN_SRC="$arg"
      ;;
  esac
done

# Con un binario ya dado como argumento, nos saltamos Go/Node y la
# compilación por completo -- BIN_SRC queda fijado aquí y los bloques de
# abajo no se ejecutan (ver el "if [ -z ... ]" que los envuelve). --web no
# puede tener efecto sobre un binario que ya viene compilado: la web solo
# se puede embeber compilando desde código, así que solo se avisa.
if [ -n "$BIN_SRC" ]; then
  [ -x "$BIN_SRC" ] || die "no encuentro un binario ejecutable en: $BIN_SRC"
  if [ "$WITH_WEB" -eq 1 ]; then
    echo "==> Aviso: --web no tiene efecto dando ya un binario compilado ($BIN_SRC)."
    echo "    Para incluir la web, o bien vuelve a lanzar este script sin darle un binario,"
    echo "    o usa nexuscloud/deploy/scripts/enable-web.sh sobre la instalación resultante."
  fi
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

# ---- 1b. Node.js + build de la web: solo con --web (secure/lean by
# default -- sin la bandera, ni se descarga Node ni se toca nexuscloud/web/,
# y el binario compilado en el paso 2 embebe solo el placeholder vacío de
# siempre). Mismo patrón que el bloque de Go: usa el Node ya instalado si
# cumple la versión mínima, si no lo instala desde el tarball oficial de
# nodejs.org (nunca el gestor de paquetes de la distro).
if [ "$WITH_WEB" -eq 1 ]; then
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
      *) die "arquitectura no reconocida para Node.js: $(uname -m) -- instala Node.js manualmente desde https://nodejs.org/ y vuelve a lanzar este script (o sin --web para seguir sin la web)." ;;
    esac
    # index.tab: texto plano separado por tabuladores (version, date, files,
    # ..., lts, security) -- a diferencia de index.json, se puede leer con
    # awk sin depender de tener jq instalado en una máquina recién instalada.
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
    echo "    instalado en /usr/local/lib/nodejs-nexuscloud -- añade .../bin a tu PATH permanentemente si vas a compilar aquí más veces"
  fi

  echo "==> Compilando la interfaz web (nexuscloud/web/)"
  (
    cd "${CODE_DIR}/web"
    npm ci --no-audit --no-fund
    npm run build
  )
  echo "    web compilada"
else
  # Sin --web, el binario debe quedar SIN web sí o sí, sin importar qué
  # hubiera antes en nexuscloud/web/dist/ (p.ej. restos de un "npm run
  # build" manual anterior, o de una instalación previa con --web en este
  # mismo checkout) -- go:embed empaqueta lo que encuentre en el disco en
  # el momento de compilar, no lo que "debería" haber según ningún flag.
  # Volver al placeholder vacío de siempre es lo que de verdad hace
  # determinista "sin --web = sin web", en vez de depender de que dist/
  # ya estuviera limpio por casualidad. go:embed all:dist (web/embed.go)
  # falla en tiempo de compilación si dist/ queda con CERO ficheros --
  # hay que garantizar que .gitkeep sobrevive (o se recrea), no solo evitar
  # borrarlo si ya estaba: un "npm run build" anterior (vite limpia
  # dist/ entero, .gitkeep incluido, antes de escribir su salida) puede
  # haberlo dejado sin él.
  find "${CODE_DIR}/web/dist" -mindepth 1 -not -name '.gitkeep' -delete
  : > "${CODE_DIR}/web/dist/.gitkeep"
fi

# ---- 2. Compilar el binario (mismos flags que nexuscloud/Dockerfile, sin
# CGO -- estático, no necesita glibc/musl en runtime). Si el paso 1b acaba
# de compilar la web en nexuscloud/web/dist, go:embed la incluye aquí; si
# no, el "else" de arriba ya dejó dist/ reducido al placeholder vacío de
# siempre (mismo binario "sin web" que ya se obtenía antes de que --web
# existiera). ---------------------------
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
    if [ "$WITH_WEB" -eq 1 ]; then
        # "config init" no tiene una bandera para esto -- se activa aquí
        # sobre el YAML ya generado, mismo criterio que la unit de systemd
        # de más abajo (sed sobre una plantilla ya escrita). El "n;s/.../"
        # solo toca la línea "enabled:" INMEDIATAMENTE debajo de "web:", no
        # cualquier otra sección que también tenga un campo "enabled".
        sed -i '/^web:/{n;s/enabled: false/enabled: true/}' "$CONFIG_FILE"
    fi
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

if [ "$WITH_WEB" -eq 1 ]; then
    web_status="incluida y activada (web.enabled: true)"
else
    web_status="NO incluida (binario sin ese código; web.enabled: false) -- para añadirla después, sudo nexuscloud/deploy/scripts/enable-web.sh"
fi

cat <<EOF

NexusCloud instalado. El servicio NO se ha arrancado todavía.
  1. Revisa   ${CONFIG_FILE}
  2. Crea un admin:  sudo -u ${NX_USER} ${NX_PREFIX}/nexuscloud --config ${CONFIG_FILE} admin create-user
  3. Arranca:  sudo systemctl start nexuscloud
  4. Estado:   systemctl status nexuscloud

Interfaz web: ${web_status}

Actualizar más adelante:  sudo nexuscloud/deploy/scripts/update.sh <nuevo-binario>
Desinstalar:              sudo nexuscloud/deploy/scripts/uninstall.sh   (añade --purge para borrar datos)
EOF
