# Stage 1: Build the Go binary
FROM cgr.dev/chainguard/go:latest AS builder

WORKDIR /app

# Copy dependency files and download modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build a statically linked binary.
# modernc.org/sqlite is pure Go, so CGO stays off and static:latest works.
COPY main.go ./
COPY pkg/ pkg/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o queue-bot .
# Pre-create the data dir: the runtime image has no shell to mkdir with.
RUN mkdir -p /app/data

# Stage 2: Create the minimal runtime container
FROM cgr.dev/chainguard/static:latest

WORKDIR /app
# Copy the compiled binary from the builder stage
COPY --from=builder /app/queue-bot /app/queue-bot
COPY --from=builder --chown=65532:65532 /app/data /app/data

# Run as the default non-root user for security
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/queue-bot"]
