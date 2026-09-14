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
# distroless "nonroot": sin shell, sin gestor de paquetes, sin root (§103,
# §104) -- reduce drásticamente la superficie de ataque del contenedor.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /

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
