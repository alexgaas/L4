# Stage 1: Builder
# Use a full Go image for building the application
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY certs/ certs/
# RUN go mod download

COPY . .

# Build the Go application as a static executable
RUN go build -o l4 .

# if you need to debug arm64 specific code
# CMD ["tail", "-f", "/dev/null"]

# Stage 2: Production
FROM scratch

WORKDIR /

COPY --from=builder /app/certs certs/
COPY --from=builder /app/quoter .

EXPOSE 8443

CMD ["./l4", "", ""]