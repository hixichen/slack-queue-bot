FROM golang:1.25-alpine AS builder

# gcc/musl-dev are required to build the CGO-based mattn/go-sqlite3 driver.
RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY pkg/ pkg/
# CGO is required by go-sqlite3. The binary links musl dynamically and runs on
# the alpine runtime below. -s -w strips debug info to shrink the image.
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o queue-bot .

# ---- runtime image ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=builder /app/queue-bot .
RUN mkdir -p /app/data && chown -R appuser:appuser /app

USER appuser
EXPOSE 8080
CMD ["./queue-bot"]
