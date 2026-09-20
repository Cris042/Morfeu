# Builder stage
# Digest do index multi-arch de golang:1.25-alpine (supply chain — PRD 0004 RNF01)
FROM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o app ./cmd/morfeu

# Runtime stage
FROM scratch

COPY --from=builder /app/app /app
# main.go roda migrations no startup via migrate.New("file://migrations", ...) —
# sem esta cópia a imagem sobe e morre com "no such file or directory".
COPY --from=builder /app/migrations /migrations

EXPOSE 8080

ENTRYPOINT ["/app"]
