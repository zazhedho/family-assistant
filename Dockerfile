# Stage 1: Build the Go application
FROM golang:latest AS builder

WORKDIR /app

# Copy go.mod and go.sum to download dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Stage 2: Create the final image
FROM alpine:latest

WORKDIR /root/

# Install required runtime packages (tzdata for timezone support)
RUN apk add --no-cache tzdata

# Copy the built binary from the builder stage
COPY --from=builder /app/main .
COPY entrypoint.sh ./entrypoint.sh

# Copy migrations
COPY migrations ./migrations

# Ensure entrypoint is executable
RUN chmod +x ./entrypoint.sh

# Expose port 8080 to the outside world
EXPOSE 8080

# Optional migration can be toggled via RUN_MIGRATION=true
ENTRYPOINT ["./entrypoint.sh"]
