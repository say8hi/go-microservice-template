# Start from minimal Go image
FROM golang:1.24-alpine

# Install necessary tools
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum first (for caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

# Build the binary
RUN go build -o main .

# Expose port (тот же что в main.go, если там используется 8080)
EXPOSE 8080

# Run binary
CMD ["./main"]
