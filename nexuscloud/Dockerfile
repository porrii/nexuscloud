# syntax=docker/dockerfile:1

# ---- frontend -----------------------------------------------------------
FROM node:24-alpine AS frontend-builder
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# ---- build ------------------------------------------------------------
FROM golang:1.25-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Sustituye el web/dist con solo el .gitkeep por el build real de la web
# (go:embed necesita contenido real en tiempo de compilación de Go).
COPY --from=frontend-builder /src/web/dist ./web/dist

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

# CGO_ENABLED=0: el driver sqlite (modernc.org/sqlite) es Go puro, así que
# el binario resultante es estático y no necesita glibc/musl en runtime.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/porrii/nexuscloud/internal/version.Version=${VERSION} \
      -X github.com/porrii/nexuscloud/internal/version.GitCommit=${GIT_COMMIT} \
      -X github.com/porrii/nexuscloud/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/nexuscloud ./cmd/nexuscloud

# El directorio de datos se crea aquí (con el propietario correcto) porque
# la imagen distroless final no tiene shell para hacer un chown en runtime.
RUN mkdir -p /data && chown 65532:65532 /data

# ---- runtime ------------------------------------------------------------
# debian:12-slim (§34, ADR-041 Decisión 0): ffmpeg/poppler-utils (miniaturas
# de vídeo/PDF) necesitan un intérprete de comandos y un gestor de paquetes
# reales -- distroless/static, la base anterior, no los tiene. Esto revierte
# parte de la reducción de superficie de ataque que esa elección buscaba
# (§103/§104): coste ACEPTADO explícitamente, no gratis -- compensado con
# usuario no-root explícito (mismo UID:GID 65532 que antes, para no romper
# el propietario de volúmenes /data ya existentes), escaneo de la imagen en
# CI (Trivy, ver .github/workflows/ci.yml) y un rebuild semanal programado
# para que las actualizaciones de seguridad de apt lleguen aunque no haya
# cambios de código -- ni govulncheck (solo Go) ni Dependabot (solo seguiría
# la línea FROM) ven nunca lo que apt-get install resuelve en cada build.
# Ver también docs/deployment.md para --security-opt=no-new-privileges/
# --read-only/--tmpfs y límites de recursos por cgroup (contención real de
# ffmpeg/pdftoppm, no reinventada desde Go).
FROM debian:12-slim
WORKDIR /

# ca-certificates: HTTPS saliente real (actualizaciones del cliente vía
# GitHub, backup remoto a otro NexusCloud) -- distroless/static lo traía
# integrado, debian:12-slim no. ffmpeg/poppler-utils: miniaturas de vídeo/
# PDF (§34 Decisión 4). --no-install-recommends: solo lo que se usa de
# verdad, ninguna de las docenas de dependencias "sugeridas" de ffmpeg
# (códecs/formatos que este proyecto nunca necesita). Caché de apt borrada
# en la misma capa para no dejarla en la imagen final.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      ffmpeg \
      poppler-utils \
    && rm -rf /var/lib/apt/lists/*

# Usuario sin privilegios explícito -- debian:12-slim no trae uno propio, a
# diferencia de distroless:nonroot. Mismo UID:GID 65532 que la imagen
# anterior a propósito: así /data conserva el mismo propietario al
# actualizar, sin chown manual en despliegues ya existentes.
RUN groupadd --system --gid 65532 nonroot \
    && useradd --uid 65532 --gid 65532 --no-create-home --shell /usr/sbin/nologin nonroot

COPY --from=builder /out/nexuscloud /usr/local/bin/nexuscloud
COPY --from=builder --chown=65532:65532 /data /data

ENV NEXUSCLOUD_DATA_DIR=/data
# La interfaz web ya viaja embebida en el binario; sigue desactivada por
# defecto (secure by default, §3/§47) hasta que se active explícitamente.
VOLUME ["/data"]
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/nexuscloud"]
CMD ["start"]
