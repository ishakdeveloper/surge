# chat. Built from the repository root:
#
#   docker build -f deploy/docker/chat.Dockerfile -t surge/chat:dev .
#
# The same shape as trip.Dockerfile, which says why: one image rather than a
# dev/prod pair, and cgo on, because the module links h3-go and a build with
# CGO_ENABLED=0 fails outright. Chat itself touches no geometry, but it is
# built from the one module that does.
FROM golang:1.25-alpine AS build

RUN apk add --no-cache build-base

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/chat ./services/chat/cmd

FROM alpine:3.21

# ca-certificates for Expo's push API, which is the one call chat makes
# outside the cluster.
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 surge

COPY --from=build /out/chat /app/chat

USER 10001

ENTRYPOINT ["/app/chat"]
