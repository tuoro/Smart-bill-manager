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
RUN npx vue-tsc -b && npx vite build

# ============================================
# Stage 2: Build Backend (Go)
# ============================================
FROM golang:1.24-alpine AS backend-builder

WORKDIR /app/backend

# Install build dependencies including Tesseract and poppler-utils
# Note: tesseract-ocr-dev requires leptonica-dev, and pkgconfig helps with library discovery
RUN apk add --no-cache gcc g++ musl-dev tesseract-ocr-dev leptonica-dev pkgconfig ca-certificates poppler-utils

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

# Install packages - split into multiple RUN commands for better debugging
# Install supervisor first and verify
RUN apk add --no-cache supervisor && \
    which supervisord && \
    ls -la $(which supervisord)

# Install core dependencies (required)
RUN apk add --no-cache \
    tesseract-ocr \
    ca-certificates \
    poppler-utils \
    poppler-data \
    imagemagick \
    python3 \
    py3-pip

# Install Tesseract language data
RUN apk add --no-cache tesseract-ocr-data-chi_sim tesseract-ocr-data-eng 2>/dev/null || \
    (apk add --no-cache wget && \
     TESSDATA_DIR=/usr/share/tessdata && \
     mkdir -p $TESSDATA_DIR && \
     wget -q -O $TESSDATA_DIR/chi_sim.traineddata \
         https://github.com/tesseract-ocr/tessdata_fast/raw/main/chi_sim.traineddata && \
     wget -q -O $TESSDATA_DIR/eng.traineddata \
         https://github.com/tesseract-ocr/tessdata_fast/raw/main/eng.traineddata && \
     apk del wget)

# Install optional libraries for RapidOCR (mesa-gl for OpenGL, glib for GLib, libstdc++ for C++ runtime)
# Allows failures since RapidOCR is optional and will fall back to Tesseract
RUN apk add --no-cache mesa-gl glib libstdc++ 2>/dev/null || true

# Install Python OCR dependencies (optional, will fall back to Tesseract if fails)
RUN python3 -m pip install --break-system-packages --upgrade pip setuptools wheel 2>/dev/null || true && \
    python3 -m pip install --break-system-packages --no-cache-dir rapidocr_onnxruntime 2>/dev/null || \
    echo "RapidOCR installation skipped - using Tesseract for OCR"

# Ensure supervisord is accessible at /usr/bin/supervisord
RUN if [ ! -f /usr/bin/supervisord ]; then \
        SUPERVISOR_PATH=$(which supervisord 2>/dev/null); \
        if [ -n "$SUPERVISOR_PATH" ]; then \
            ln -sf "$SUPERVISOR_PATH" /usr/bin/supervisord; \
        fi; \
    fi && \
    test -x /usr/bin/supervisord || (echo "supervisord not found!" && exit 1)

WORKDIR /app

# Copy built backend binary
COPY --from=backend-builder /app/backend/server ./backend/server

# Copy built frontend files to nginx html directory
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html

# Create necessary directories for backend
RUN mkdir -p /app/backend/uploads /app/backend/data /app/scripts

# Copy OCR scripts
COPY scripts/paddleocr_cli.py /app/scripts/

# Copy nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Create supervisord configuration
COPY supervisord.conf /etc/supervisord.conf

# Create log directories
RUN mkdir -p /var/log/supervisor

# Expose port 80 (nginx serves both frontend and proxies to backend)
EXPOSE 80

# Set environment variables
ENV PORT=3001

# Start supervisord which manages both nginx and Go backend
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]
