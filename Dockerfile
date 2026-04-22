# ============================
# Stage 1: Build
# ============================
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go files
COPY go.mod ./
RUN go mod download

COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o chatbot-server .

# ============================
# Stage 2: Runtime (minimal)
# ============================
FROM alpine:latest

WORKDIR /app

# Copy binary dari builder
COPY --from=builder /app/chatbot-server .

# Expose port (Railway akan override via ENV PORT)
EXPOSE 8080

CMD ["./chatbot-server"]
