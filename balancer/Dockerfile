# Stage 1: Builder
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o balancer main.go

# Stage 2: Runtime
FROM alpine:latest

WORKDIR /root/

# Copy the binary from the builder
COPY --from=builder /app/balancer .
# Copy default config just in case
COPY config.yaml .

EXPOSE 2050 8443

# The balancer needs to be run as root to use RAW sockets, 
# which is the default in Docker.
ENTRYPOINT ["./balancer"]
CMD ["-config", "config.yaml"]
