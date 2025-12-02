# Unified Dockerfile for Smart Bill Manager
# This builds both frontend and backend (Go) into a single image

# ============================================
# Stage 1: Build Frontend
# ============================================
FROM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

# Copy frontend package files
COPY frontend/package*.json ./

# Install frontend dependencies
RUN npm ci

# Copy frontend source code
COPY frontend/ .

# Build the frontend (use npx to ensure local binaries are found)
RUN npx tsc -b && npx vite build

# ============================================
# Stage 2: Build Backend (Go)
# ============================================
FROM golang:1.23-alpine AS backend-builder

WORKDIR /app/backend

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go mod files
COPY backend-go/go.mod backend-go/go.sum ./

# Download dependencies
RUN go mod download

# Copy backend source code
COPY backend-go/ .

# Build the Go binary
RUN CGO_ENABLED=1 GOOS=linux go build -o server ./cmd/server

# ============================================
# Stage 3: Production Image
# ============================================
FROM nginx:alpine AS production

# Install supervisor and SQLite runtime
RUN apk add --no-cache supervisor

WORKDIR /app

# Copy built backend binary
COPY --from=backend-builder /app/backend/server ./backend/server

# Copy built frontend files to nginx html directory
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html

# Create necessary directories for backend
RUN mkdir -p /app/backend/uploads /app/backend/data

# Create nginx configuration
RUN mkdir -p /etc/nginx/http.d
COPY nginx.conf /etc/nginx/http.d/default.conf

# Create supervisord configuration
COPY supervisord.conf /etc/supervisord.conf

# Create log directories
RUN mkdir -p /var/log/supervisor

# Expose port 80 (nginx serves both frontend and proxies to backend)
EXPOSE 80

# Set environment variables
ENV NODE_ENV=production
ENV PORT=3001

# Start supervisord which manages both nginx and Go backend
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]
