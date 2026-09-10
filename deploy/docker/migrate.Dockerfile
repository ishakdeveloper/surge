# The migration runner, shipped as its own image so a release applies migrations
# as an explicit step rather than at service boot — two instances starting
# together would otherwise both migrate.
FROM golang:1.25-alpine AS build

RUN apk add --no-cache build-base
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./tools/migrate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 surge
COPY --from=build /out/migrate /app/migrate
USER 10001
ENTRYPOINT ["/app/migrate"]
