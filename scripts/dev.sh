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

cmd="${1:-all}"
case "$cmd" in
  build)  run "go build ./..." ;;
  vet)    run "go vet ./..." ;;
  test)   run "go test ./... ${*:2}" ;;
  fmt)    run "gofmt -w . && gofmt -l ." ;;
  tidy)   run "go mod tidy" ;;
  all)    run "go mod tidy && go build ./... && go vet ./... && go test ./... && gofmt -l ." ;;
  *)
    echo "Uso: $0 [build|vet|test|fmt|tidy|all]" >&2
    exit 1
    ;;
esac
