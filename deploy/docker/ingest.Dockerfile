# ingest. Built from the repository root:
#
#   docker build -f deploy/docker/ingest.Dockerfile -t surge/ingest .
#
# One Dockerfile rather than the usual dev/prod pair, and the reason is cgo.
#
# The reference this project borrows its layout from keeps a second, trivial dev
# image that copies a host-compiled binary in, so Tilt can rebuild in a second
# and live-update the file. That works when the code is pure Go. It does not
# work here: h3-go is a cgo binding to the H3 C library, so a macOS host cannot
# cross-compile a Linux binary without a full cross toolchain — and building
# with CGO_ENABLED=0 fails outright with "build constraints exclude all Go
# files", which is the compiler saying the geospatial core is not optional.
#
# Instead the module and build caches are mounted, which makes an incremental
# rebuild a few seconds rather than a minute. The genuinely fast loop is still
# running the services on the host, which is what `make dev-*` does.
FROM golang:1.25-alpine AS build

# build-base for the C compiler h3-go needs.
RUN apk add --no-cache build-base

WORKDIR /src

# Manifests first, so the download layer is cached until dependencies actually
# change rather than on every source edit.
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./

# Symbols and DWARF stripped: they are a third of the image, and a panic still
# carries function names.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ingest ./services/ingest/cmd

FROM alpine:3.21

# The binary is dynamically linked against musl, which is why the runtime is
# alpine and not scratch or distroless.
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 surge

COPY --from=build /out/ingest /app/ingest

# Unprivileged: nothing here needs root, and a container running as root is one
# escape away from being a host compromise.
USER 10001

ENTRYPOINT ["/app/ingest"]
