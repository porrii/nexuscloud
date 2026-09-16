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
# Uso normal (interactivo): clona el repo, y desde su raíz:
#   sudo ./install.sh
# Con una terminal real por delante, el script pregunta lo poco que hace
# falta (interfaz web sí/no, usuario y contraseña del administrador) y al
# terminar deja NexusCloud funcionando de verdad -- admin creado, servicio
# arrancado, sin ningún paso manual más. Pensado para poder instalarlo sin
# saber de informática: si los valores por defecto ya valen, basta con
# pulsar Enter en cada pregunta salvo la de la contraseña.
#
# Uso avanzado / scriptado (sin preguntas):
#   sudo ./install.sh --unattended        # comportamiento clásico: sin admin, sin arrancar
#   sudo NX_ADMIN_USERNAME=admin NX_ADMIN_PASSWORD=... ./install.sh
#                                          # crea admin y arranca sin preguntar nada
#   sudo ./install.sh --web               # incluye también la interfaz web
#   sudo ./install.sh /ruta/al/binario    # usa un binario ya compilado,
#                                          # se salta Go y la compilación
#                                          # (--web no tiene efecto en este caso: ver más abajo)
#
# --web es opt-in a propósito (secure/lean by default, igual criterio que
# el resto del proyecto) EN EL CAMINO SCRIPTADO -- en el camino
# interactivo, en cambio, se pregunta con "sí" como respuesta por defecto,
# porque sin ella no hay forma de usar NexusCloud sin la línea de
# comandos. Sin la web (ni por pregunta ni por --web), el binario embebe
# solo un placeholder vacío (0 bytes de JS/CSS), y `web.enabled` queda en
# `false`. Añadir la web más adelante a una instalación ya hecha, sin
# reinstalar desde cero: nexuscloud/deploy/scripts/enable-web.sh
#
# Variables de entorno (todas opcionales, valores por defecto razonables):
#   NX_PREFIX         (por defecto /usr/local/bin)   -- dónde va el binario
#   NX_CONFIG_DIR     (por defecto /etc/nexuscloud)
#   NX_DATA_DIR       (por defecto /var/lib/nexuscloud)
#   NX_USER           (por defecto nexuscloud)       -- usuario de servicio
#   NX_PORT           (por defecto 8080)
#   NX_ADMIN_USERNAME / NX_ADMIN_PASSWORD (vacías por defecto) -- si se dan
#     AMBAS, el script crea ese administrador y arranca el servicio sin
#     preguntar nada (ni siquiera con una terminal interactiva por
#     delante) -- el camino para aprovisionar NexusCloud desde otra
#     herramienta sin intervención humana.
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
NX_PORT="${NX_PORT:-8080}"
NX_ADMIN_USERNAME="${NX_ADMIN_USERNAME:-}"
NX_ADMIN_PASSWORD="${NX_ADMIN_PASSWORD:-}"
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

# Bucle de argumentos: --web/--unattended son banderas (en cualquier
# posición), y como mucho un positional (BIN_SRC, ruta a un binario ya
# compilado).
WITH_WEB=0
UNATTENDED=0
BIN_SRC=""
for arg in "$@"; do
  case "$arg" in
    --web)
      WITH_WEB=1
      ;;
    --unattended)
      UNATTENDED=1
      ;;
    -*)
      die "opción no reconocida: $arg (uso: ./install.sh [--web] [--unattended] [/ruta/al/binario])"
      ;;
    *)
      [ -z "$BIN_SRC" ] || die "solo se admite una ruta de binario (ya se dio: $BIN_SRC)"
      BIN_SRC="$arg"
      ;;
  esac
done

# ---- 0. Preguntas interactivas (nuevo) -------------------------------
# Con NX_ADMIN_USERNAME + NX_ADMIN_PASSWORD ya fijadas por entorno, no se
# pregunta nada: es la señal de que esto se está aprovisionando desde
# fuera (otro script, una herramienta de despliegue). Si no, y hay una
# terminal real por delante (nunca si viene de un pipe, p.ej. `curl | sh`
# -- ahí `[ -t 0 ]` es falso) y no se pidió --unattended, se pregunta lo
# mínimo: interfaz web, usuario y contraseña del administrador. El
# puerto/carpeta de datos se quedan en su valor por defecto (o el que ya
# se haya fijado por variable de entorno) -- son conceptos que solo hacen
# falta preguntar a quien ya sabe que existen.
have_admin_creds() { [ -n "$NX_ADMIN_USERNAME" ] && [ -n "$NX_ADMIN_PASSWORD" ]; }

ask_yes_no() {
  local prompt="$1" default="$2" ans hint
  if [ "$default" = "S" ]; then hint="S/n"; else hint="s/N"; fi
  printf '%s [%s]: ' "$prompt" "$hint" >&2
  read -r ans
  ans="${ans:-$default}"
  case "$ans" in
    [Ss]*) return 0 ;;
    *) return 1 ;;
  esac
}

ask_username() {
  local default="$1" ans
  while true; do
    printf 'Nombre de usuario para el administrador [%s]: ' "$default" >&2
    read -r ans
    ans="${ans:-$default}"
    case "$ans" in
      *[[:space:]]*|'') echo "    no puede estar vacío ni tener espacios." >&2 ;;
      *) printf '%s' "$ans"; return 0 ;;
    esac
  done
}

ask_password() {
  local pw1 pw2
  while true; do
    printf 'Contraseña para ese usuario (mínimo 8 caracteres): ' >&2
    read -rs pw1; echo >&2
    if [ "${#pw1}" -lt 8 ]; then
      echo "    demasiado corta -- necesita al menos 8 caracteres." >&2
      continue
    fi
    printf 'Repite la contraseña: ' >&2
    read -rs pw2; echo >&2
    if [ "$pw1" != "$pw2" ]; then
      echo "    no coincide con la anterior, vuelve a intentarlo." >&2
      continue
    fi
    printf '%s' "$pw1"
    return 0
  done
}

if have_admin_creds; then
  echo "==> NX_ADMIN_USERNAME/NX_ADMIN_PASSWORD ya fijadas -- se usan sin preguntar nada"
elif [ "$UNATTENDED" -eq 0 ] && [ -t 0 ]; then
  echo "==> Unas pocas preguntas antes de empezar (Enter acepta el valor por defecto)"
  # Con un binario ya dado (positional), --web no tiene efecto (aviso más
  # abajo) -- ni tiene sentido preguntar algo que se va a ignorar.
  if [ "$WITH_WEB" -eq 0 ] && [ -z "$BIN_SRC" ]; then
    ask_yes_no "¿Instalar también la interfaz web (para usar NexusCloud desde el navegador)?" "S" && WITH_WEB=1
  fi
  NX_ADMIN_USERNAME="$(ask_username "admin")"
  NX_ADMIN_PASSWORD="$(ask_password)"
  echo "==> Listo -- a partir de aquí no hace falta nada más, puede tardar unos minutos"
else
  echo "==> Sin terminal interactiva y sin NX_ADMIN_USERNAME/NX_ADMIN_PASSWORD: modo clásico (sin crear admin, sin arrancar el servicio)"
fi

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
    if [ "$NX_PORT" != "8080" ]; then
        # A diferencia de "web:" (un único campo, por eso ahí sí vale
        # "la línea siguiente a la cabecera"), "server:" tiene varios
        # campos y "port" no es el primero (host va antes) -- hace falta
        # buscar la línea del puerto en sí. Sin anclar la indentación
        # exacta (probado real: yaml.v3 la escribe con 4 espacios, no 2 --
        # un `^  port: 8080$` con 2 espacios asumidos NUNCA hace match y
        # falla en silencio, encontrado ejecutando esto de verdad contra
        # un config.yaml real, no contra uno fabricado a mano). "8080"
        # solo aparece una vez en todo config.yaml (confirmado), así que
        # un simple substring basta, mismo criterio que ya usa el sed de
        # "web:" un poco más arriba.
        sed -i "s/port: 8080/port: ${NX_PORT}/" "$CONFIG_FILE"
    fi
    if [ "$NX_DATA_DIR" != "/var/lib/nexuscloud" ]; then
        # Bug real preexistente, encontrado probando de verdad
        # NX_DATA_DIR distinto del de siempre (no algo que introduzca este
        # cambio): "config init" no tiene ninguna bandera para fijar
        # storage.dataDir, así que SIEMPRE escribía el valor por defecto
        # (/var/lib/nexuscloud) en el YAML sin importar qué NX_DATA_DIR se
        # hubiera pedido -- el binario de verdad (systemctl start, no solo
        # este script) leía ese default, no la carpeta que se le pidió,
        # aunque los directorios/permisos SÍ se crearan en el sitio
        # correcto. "/var/lib/nexuscloud" solo aparece una vez en todo
        # config.yaml (confirmado).
        sed -i "s#dataDir: /var/lib/nexuscloud#dataDir: ${NX_DATA_DIR}#" "$CONFIG_FILE"
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
if [ "$NX_USER" != "nexuscloud" ]; then
    # Otro bug real preexistente, encontrado por el mismo motivo que los
    # de arriba (probar de verdad un NX_USER distinto del de siempre, no
    # algo que introduzca este cambio): sin esto, el servicio seguía
    # arrancando como el usuario "nexuscloud" de siempre aunque se hubiera
    # creado (y se le hubiera dado permisos sobre config/datos) un usuario
    # de servicio distinto -- "Permission denied" leyendo su propio
    # config.yaml (0640, propiedad de NX_USER), confirmado con journalctl
    # de verdad, no en teoría.
    sed -i "s#^User=nexuscloud#User=${NX_USER}#;s#^Group=nexuscloud#Group=${NX_USER}#" /etc/systemd/system/nexuscloud.service
fi
sed -i "s#ExecStart=/usr/local/bin/nexuscloud start --config /etc/nexuscloud/config.yaml#ExecStart=${NX_PREFIX}/nexuscloud start --config ${CONFIG_FILE}#" /etc/systemd/system/nexuscloud.service
systemctl daemon-reload
systemctl enable nexuscloud >/dev/null

if [ "$WITH_WEB" -eq 1 ]; then
    web_status="incluida y activada (web.enabled: true)"
else
    web_status="NO incluida (binario sin ese código; web.enabled: false) -- para añadirla después, sudo nexuscloud/deploy/scripts/enable-web.sh"
fi

# ---- 4. Crear el administrador y arrancar (nuevo) -- solo si tenemos
# credenciales (por pregunta o por NX_ADMIN_USERNAME/NX_ADMIN_PASSWORD).
# Sin ellas, se conserva EXACTAMENTE el final de siempre: nada se crea, el
# servicio no se arranca, y se listan los pasos para hacerlo a mano.
lan_ip() {
    hostname -I 2>/dev/null | awk '{print $1}' || true
}

if have_admin_creds; then
    echo "==> Creando el administrador '${NX_ADMIN_USERNAME}'"
    # La contraseña va por stdin, nunca como argumento de la línea de
    # comandos (visible para cualquier otro proceso vía la lista de
    # procesos, p.ej. "ps aux") -- admin.go ya soporta leerla así cuando
    # --password se omite y stdin no es un terminal (mismo camino que ya
    # usa promptPassword, ver internal/cli/helpers.go).
    printf '%s\n' "$NX_ADMIN_PASSWORD" | sudo -u "$NX_USER" "${NX_PREFIX}/nexuscloud" --config "$CONFIG_FILE" admin create-user \
        --username "$NX_ADMIN_USERNAME"

    echo "==> Arrancando el servicio"
    systemctl start nexuscloud
    sleep 1

    health_ok=0
    for _ in 1 2 3 4 5; do
        if curl -fsS "http://127.0.0.1:${NX_PORT}/health" >/dev/null 2>&1; then
            health_ok=1
            break
        fi
        sleep 1
    done

    echo
    if [ "$health_ok" -eq 1 ]; then
        ip="$(lan_ip)"
        echo "NexusCloud está funcionando."
        echo "  Abre esto en tu navegador:  http://localhost:${NX_PORT}"
        [ -n "$ip" ] && echo "  O desde otro equipo de tu red:  http://${ip}:${NX_PORT}"
        echo "  Usuario:  ${NX_ADMIN_USERNAME}   (la contraseña es la que has elegido)"
    else
        echo "NexusCloud se instaló, pero el servicio no respondió a tiempo en http://127.0.0.1:${NX_PORT}/health."
        echo "  Revisa qué pasó:  systemctl status nexuscloud   /   journalctl -u nexuscloud -n 50"
        echo "  Diagnóstico completo:  sudo -u ${NX_USER} ${NX_PREFIX}/nexuscloud --config ${CONFIG_FILE} doctor"
    fi
    echo
    echo "Interfaz web: ${web_status}"
    echo
    echo "Actualizar más adelante:  sudo nexuscloud/deploy/scripts/update.sh <nuevo-binario>"
    echo "Desinstalar:              sudo nexuscloud/deploy/scripts/uninstall.sh   (añade --purge para borrar datos)"
else
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
fi
