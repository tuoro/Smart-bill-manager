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

# Install supervisor, SQLite runtime, Tesseract with language packs, poppler-utils, ImageMagick, poppler-data, Python and OCR dependencies
# Note: Alpine's tesseract-ocr package includes basic language data
# Additional language data can be installed via tesseract-ocr-data-* packages or downloaded manually
# poppler-data provides CJK CMap files for better PDF text extraction
# imagemagick provides image preprocessing capabilities for better OCR
# Python 3 and pip are required for OCR CLI integration
RUN apk add --no-cache supervisor tesseract-ocr ca-certificates poppler-utils poppler-data imagemagick \
    python3 py3-pip \
    libgl libglib libgomp libstdc++ && \
    apk add --no-cache tesseract-ocr-data-chi_sim tesseract-ocr-data-eng 2>/dev/null || \
    (apk add --no-cache wget && \
     TESSDATA_DIR=/usr/share/tessdata && \
     mkdir -p $TESSDATA_DIR && \
     wget -q -O $TESSDATA_DIR/chi_sim.traineddata \
         https://github.com/tesseract-ocr/tessdata_fast/raw/main/chi_sim.traineddata && \
     wget -q -O $TESSDATA_DIR/eng.traineddata \
         https://github.com/tesseract-ocr/tessdata_fast/raw/main/eng.traineddata && \
     apk del wget)

# Upgrade pip and install RapidOCR (lightweight OCR alternative)
# RapidOCR is more compatible with Alpine Linux than PaddlePaddle
# If installation fails, the build continues and system falls back to PaddleOCR if available, or Tesseract
RUN pip3 install --upgrade pip setuptools wheel && \
    (pip3 install --no-cache-dir rapidocr_onnxruntime || \
     echo "RapidOCR installation failed, OCR will fall back to PaddleOCR if available, or Tesseract")

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
