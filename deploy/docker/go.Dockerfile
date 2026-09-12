# Every Go image, from one file. Built from the repository root:
#
#   docker build -f deploy/docker/go.Dockerfile \
#     --build-arg PACKAGE=services/gateway/cmd -t surge/gateway .
#
# One file rather than one per service, because they differed in nothing but
# the package path — and six copies of a build are six places for a pinned
# version or a reproducibility fix to be applied to five.
#
# One Dockerfile rather than the usual dev/prod pair, and the reason is cgo.
# h3-go is a cgo binding to the H3 C library, so a macOS host cannot
# cross-compile a Linux binary without a full cross toolchain — and building
# with CGO_ENABLED=0 fails outright with "build constraints exclude all Go
# files", which is the compiler saying the geospatial core is not optional.
# The module and build caches are mounted instead, which makes an incremental
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

# Declared after the copy, so choosing a different service reuses every layer
# above rather than downloading the modules again.
ARG PACKAGE

# -trimpath, and .git is not in the build context, so nothing machine- or
# commit-specific reaches the binary: the same source builds the same bytes,
# which is what lets an unchanged service keep its digest and not roll.
# Symbols and DWARF stripped: they are a third of the image, and a panic still
# carries function names.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    test -n "$PACKAGE" || { echo "build with --build-arg PACKAGE=services/<name>/cmd"; exit 1; } && \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service "./$PACKAGE"

FROM alpine:3.21

# The binary is dynamically linked against musl, which is why the runtime is
# alpine and not scratch or distroless.
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 surge

COPY --from=build /out/service /app/service

# Unprivileged: nothing here needs root, and a container running as root is one
# escape away from being a host compromise.
USER 10001

ENTRYPOINT ["/app/service"]
