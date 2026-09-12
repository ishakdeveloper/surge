# fleet. Built from the repository root:
#
#   docker build -f deploy/docker/fleet.Dockerfile -t surge/fleet:dev .
#
# The same shape as trip.Dockerfile, which says why: one image rather than a
# dev/prod pair, and cgo on, because the module links h3-go and a build with
# CGO_ENABLED=0 fails outright.
FROM golang:1.25-alpine AS build

RUN apk add --no-cache build-base

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fleet ./services/fleet/cmd

FROM alpine:3.21

# ca-certificates for the three things this service calls outside the cluster:
# Stripe Identity, the RDW's open data, and the documents bucket.
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 surge

COPY --from=build /out/fleet /app/fleet

USER 10001

ENTRYPOINT ["/app/fleet"]
