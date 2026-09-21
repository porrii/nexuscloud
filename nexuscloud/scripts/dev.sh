#!/usr/bin/env bash
# Envoltorio de desarrollo: compila/testea NexusCloud dentro de un
# contenedor golang:1.25-bookworm, útil en máquinas sin Go instalado
# localmente (así se desarrolló la Fase 1). Si tienes Go 1.25+ nativo,
# usa directamente `go build|vet|test ./...` — es más rápido.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

IMAGE="golang:1.25-bookworm"
GOMOD_VOLUME="nexuscloud-gomod"
GOCACHE_VOLUME="nexuscloud-gocache"

run() {
  MSYS_NO_PATHCONV=1 docker run --rm \
    -v "$(pwd)":/app -w /app \
    -v "${GOMOD_VOLUME}":/go/pkg/mod \
    -v "${GOCACHE_VOLUME}":/root/.cache/go-build \
    "${IMAGE}" bash -c "$1"
}

# test-mysql/test-mariadb/test-postgres (ADR-031): levantan un contenedor
# real del motor indicado en una red Docker propia, esperan a que acepte
# conexiones autenticadas de verdad (retry de conexión real -- un
# healthcheck de "puerto abierto" da falso positivo mientras la contraseña
# root todavía se está aplicando), corren `go test ./...` dentro del mismo
# contenedor golang ya usado arriba (conectado a esa misma red) con
# NEXUSCLOUD_TEST_<DRIVER>_DSN apuntando al motor real, y limpian
# contenedor+red al terminar -- éxito o fallo, vía `trap`. No están en
# `all`: son más lentos y necesitan Docker -- se invocan explícitamente.
# driver: nombre del driver NexusCloud (mysql|postgres, para el nombre de
# la variable de entorno). engine: nombre corto para los recursos Docker
# (permite mysql/mariadb, dos motores distintos, comparar bajo driver=mysql).
run_db_test() {
  local driver="$1" engine="$2" image="$3" dsn="$4" ready_cmd="$5"; shift 5
  local net="nexuscloud-test-${engine}-net"
  local db_container="nexuscloud-test-${engine}-db"
  local env_var="NEXUSCLOUD_TEST_$(echo "$driver" | tr '[:lower:]' '[:upper:]')_DSN"

  # El trap se instala con el comando ya expandido (comillas dobles): al
  # dispararse en EXIT, las locales de esta función ya no existen -- ni tras
  # un éxito ni tras un fallo --, así que no puede depender de ellas. Antes
  # usaba "${var:-}" para esquivar el "unbound variable" de `set -u`, pero eso
  # dejaba la limpieza en un `docker rm -f ""` inofensivo y el contenedor de
  # BD seguía vivo tras cada ejecución correcta. `-v` retira además el
  # volumen anónimo con los datos de la BD (con `rm -f` a secas se acumulaba).
  cleanup_cmd="MSYS_NO_PATHCONV=1 docker rm -f -v '${db_container}' >/dev/null 2>&1 || true; MSYS_NO_PATHCONV=1 docker network rm '${net}' >/dev/null 2>&1 || true"
  trap "$cleanup_cmd" EXIT
  eval "$cleanup_cmd" # por si quedó algo de una ejecución anterior interrumpida

  MSYS_NO_PATHCONV=1 docker network create "$net" >/dev/null
  MSYS_NO_PATHCONV=1 docker run -d --rm --name "$db_container" --network "$net" "$@" "$image" >/dev/null

  echo "Esperando a que $engine acepte conexiones..." >&2
  local i=0
  until MSYS_NO_PATHCONV=1 docker exec "$db_container" sh -c "$ready_cmd" >/dev/null 2>&1; do
    i=$((i+1))
    if [ "$i" -gt 60 ]; then
      echo "$engine no respondió tras 2 minutos" >&2
      exit 1
    fi
    sleep 2
  done

  MSYS_NO_PATHCONV=1 docker run --rm \
    --network "$net" \
    -v "$(pwd)":/app -w /app \
    -v "${GOMOD_VOLUME}":/go/pkg/mod \
    -v "${GOCACHE_VOLUME}":/root/.cache/go-build \
    -e "${env_var}=${dsn}" \
    "${IMAGE}" bash -c "go test ./..."
}

cmd="${1:-all}"
case "$cmd" in
  build)  run "go build ./..." ;;
  vet)    run "go vet ./..." ;;
  test)   run "go test ./... ${*:2}" ;;
  fmt)    run "gofmt -w . && gofmt -l ." ;;
  tidy)   run "go mod tidy" ;;
  all)    run "go mod tidy && go build ./... && go vet ./... && go test ./... && gofmt -l ." ;;
  test-mysql)
    # ready_cmd usa una consulta autenticada real, no "mysqladmin ping":
    # ping da falso positivo mientras la contraseña root todavía se está
    # terminando de aplicar (verificado en esta misma máquina -- ping
    # devuelve éxito y el login real falla con "Access denied" varios
    # segundos después de que ping ya diera luz verde).
    run_db_test mysql mysql mysql:8 \
      "root:nexuscloud@tcp(nexuscloud-test-mysql-db:3306)/nexuscloud" \
      "mysql -uroot -pnexuscloud -e 'SELECT 1' nexuscloud" \
      -e MYSQL_ROOT_PASSWORD=nexuscloud -e MYSQL_DATABASE=nexuscloud
    ;;
  test-mariadb)
    # El cliente CLI de las imágenes mariadb recientes se llama "mariadb",
    # no "mysql" (verificado: mysql da "executable file not found").
    run_db_test mysql mariadb mariadb:11 \
      "root:nexuscloud@tcp(nexuscloud-test-mariadb-db:3306)/nexuscloud" \
      "mariadb -uroot -pnexuscloud -e 'SELECT 1' nexuscloud" \
      -e MARIADB_ROOT_PASSWORD=nexuscloud -e MARIADB_DATABASE=nexuscloud
    ;;
  test-postgres)
    run_db_test postgres postgres postgres:16 \
      "postgres://postgres:nexuscloud@nexuscloud-test-postgres-db:5432/nexuscloud?sslmode=disable" \
      "pg_isready -U postgres" \
      -e POSTGRES_PASSWORD=nexuscloud -e POSTGRES_DB=nexuscloud
    ;;
  *)
    echo "Uso: $0 [build|vet|test|fmt|tidy|all|test-mysql|test-mariadb|test-postgres]" >&2
    exit 1
    ;;
esac
