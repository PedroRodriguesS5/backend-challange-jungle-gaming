# ==============================================================================
# Stage 1: Builder
# ==============================================================================
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache Go modules dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile static binaries with zero CGO dependencies
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/goose github.com/pressly/goose/v3/cmd/goose

# ==============================================================================
# Stage 2: Runtime
# ==============================================================================
FROM alpine:3.20 AS runtime

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy compiled binaries from builder
COPY --from=builder /bin/api /bin/api
COPY --from=builder /bin/worker /bin/worker
COPY --from=builder /bin/goose /bin/goose

# Copy database migrations
COPY migrations /migrations

EXPOSE 8080

ENTRYPOINT ["/bin/api"]
