### Base: shared dependecies between dev and build
FROM golang:1.26-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

### Migrate: runs goose migrations against the target DB
FROM base AS migrate
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
COPY migrations /migrations
ENTRYPOINT [ "goose", "-dir", "/migrations", "postgres" ]

### Dev: hot reload with air
FROM base AS dev
RUN go install github.com/air-verse/air@latest
RUN go install github.com/swaggo/swag/cmd/swag@latest
COPY . .
CMD ["air", "-c", ".air.toml"]

### Builder: compiles a static optimized binary
FROM base AS builder
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/api ./cmd/api

### Prod: minimal runtime, no shell, non-root user
FROM gcr.io/distroless/static-debian12:nonroot AS prod
COPY --from=builder /bin/api /api
# static assets (e.g. the default avatar served by /v1/users/{id}/profile_pic).
# Working dir is /, so the default AVATARS_DIR "./static/avatars" resolves here.
COPY --from=builder /app/static /static
USER nonroot:nonroot
ENTRYPOINT ["/api"]