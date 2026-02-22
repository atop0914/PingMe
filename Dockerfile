# Multi-stage Dockerfile for PingMe
# Stage 1: Builder (only needed if building from source inside Docker)
# For production, use the pre-built binary approach below

# Runtime stage - use pre-built binary
FROM alpine:3.19

# Install CA certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user for security
RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

# Create config directory
RUN mkdir -p /app/config && chown -R appuser:appgroup /app

# Copy pre-built binary (should be built locally first)
COPY --chown=appuser:appgroup pingme-server .

# Copy default config
COPY --chown=appuser:appgroup config/local.yml /app/config/local.yml

# Switch to non-root user
USER appuser

# Expose ports
# HTTP API & WebSocket
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["/app/pingme-server"]
