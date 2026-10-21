# syntax=docker/dockerfile:1
# Multi-arch: docker buildx build --platform linux/amd64,linux/arm64 -t yimeng-core .
# Cross-compiles on the build host (no QEMU needed for Go); pure-Go SQLite => CGO_ENABLED=0.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/yimeng-core ./cmd/core \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12
# Default non-root user 1000:1000. Override at run time with compose `user:` /
# `docker run --user` (e.g. Synology "1026:100", fnOS "1000:1001"). The data
# volume must be writable by that uid; the music mount only needs to be readable.
ARG UID=1000 GID=1000
COPY --from=build /out/yimeng-core /usr/local/bin/yimeng-core
COPY --from=build --chown=${UID}:${GID} /out/data /var/yimeng
COPY configs/default.toml /etc/yimeng/config.toml
ENV YIMENG_CONFIG=/etc/yimeng/config.toml
USER ${UID}:${GID}
EXPOSE 8741
VOLUME ["/var/yimeng"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/usr/local/bin/yimeng-core", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/yimeng-core"]
