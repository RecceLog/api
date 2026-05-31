### Base: shared dependecies between dev and build
FROM golang:1.26-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

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
USER nonroot:nonroot
ENTRYPOINT ["/api"]