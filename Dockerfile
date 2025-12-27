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
FROM golang:1.24 AS backend-builder

WORKDIR /app/backend

# Install build dependencies (CGO is required for sqlite)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

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
FROM nginx:stable AS production

# Install runtime dependencies (Debian-based image).
# Note: onnxruntime wheels are built for glibc; Alpine (musl) often fails to install/build them.
RUN apt-get update && apt-get install -y --no-install-recommends \
    supervisor \
    ca-certificates \
    poppler-utils \
    python3 \
    python3-pip \
    python3-venv \
    python-is-python3 \
    libgomp1 \
    libstdc++6 \
    libgl1 \
    libglib2.0-0 \
    libtbb12 \
    ocl-icd-libopencl1 \
    && rm -rf /var/lib/apt/lists/*

# Optional Intel iGPU runtime for OpenVINO GPU plugin (UHD 630, etc).
# NOTE: This may require Debian non-free repositories, so it is disabled by default to keep CI builds stable.
ARG ENABLE_INTEL_GPU_RUNTIME=false
RUN if [ "$ENABLE_INTEL_GPU_RUNTIME" = "true" ]; then \
      set -eux; \
      try_install_debian() { \
        apt-get update -o Acquire::Retries=3 || return 1; \
        # OpenCL ICD (required for many iGPU paths) + clinfo for easy diagnostics. \
        apt-get install -y --no-install-recommends intel-opencl-icd clinfo || \
          apt-get install -y --no-install-recommends intel-opencl-icd || return 1; \
        # Level Zero runtime (optional, package names vary by distro). \
        apt-get install -y --no-install-recommends level-zero libze1 || true; \
        apt-get install -y --no-install-recommends intel-level-zero-gpu || true; \
        return 0; \
      }; \
      enable_nonfree() { \
        if [ -f /etc/apt/sources.list.d/debian.sources ]; then \
          sed -i -E 's/^(Components:).*/\\1 main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources || true; \
        fi; \
        if [ -f /etc/apt/sources.list ]; then \
          sed -i -E 's/^(deb\\s+[^ ]+\\s+[^ ]+\\s+)main(\\s*)$/\\1main contrib non-free non-free-firmware\\2/' /etc/apt/sources.list || true; \
        fi; \
      }; \
      try_install_oneapi_opencl() { \
        apt-get update -o Acquire::Retries=3 || return 1; \
        apt-get install -y --no-install-recommends gnupg || return 1; \
        mkdir -p /etc/apt/keyrings; \
        python3 -c "import urllib.request; urllib.request.urlretrieve('https://repositories.intel.com/oneapi/gpgkey','/etc/apt/keyrings/intel-oneapi.asc')" || return 1; \
        gpg --dearmor -o /etc/apt/keyrings/intel-oneapi.gpg /etc/apt/keyrings/intel-oneapi.asc || return 1; \
        rm -f /etc/apt/keyrings/intel-oneapi.asc; \
        echo "deb [signed-by=/etc/apt/keyrings/intel-oneapi.gpg] https://apt.repos.intel.com/oneapi all main" > /etc/apt/sources.list.d/intel-oneapi.list; \
        rm -rf /var/lib/apt/lists/*; \
        apt-get update -o Acquire::Retries=3 || return 1; \
        apt-get install -y --no-install-recommends intel-oneapi-runtime-opencl clinfo || \
          apt-get install -y --no-install-recommends intel-oneapi-runtime-opencl || return 1; \
        return 0; \
      }; \
      if ! try_install_debian; then \
        echo "Intel GPU runtime install failed; enabling contrib/non-free and retrying..." >&2; \
        enable_nonfree; \
        rm -rf /var/lib/apt/lists/*; \
        if ! try_install_debian; then \
          echo "Intel GPU runtime still unavailable from Debian repos; trying Intel oneAPI OpenCL runtime..." >&2; \
          rm -rf /var/lib/apt/lists/*; \
          if ! try_install_oneapi_opencl; then \
            echo "WARNING: Failed to install Intel GPU runtime in image; OpenVINO will run on CPU only." >&2; \
          fi; \
        fi; \
      fi; \
      rm -rf /var/lib/apt/lists/*; \
    fi

# Install Python OCR dependencies (RapidOCR v3) in a virtualenv to avoid Debian PEP 668 restrictions.
RUN python3 -m venv /opt/venv
ENV VIRTUAL_ENV=/opt/venv
ENV PATH="/opt/venv/bin:${PATH}"
RUN ln -sf /opt/venv/bin/python3 /opt/venv/bin/python
RUN /opt/venv/bin/python3 -m pip install --no-cache-dir --upgrade pip setuptools wheel && \
    /opt/venv/bin/python3 -m pip install --no-cache-dir "rapidocr==3.*" onnxruntime && \
    /opt/venv/bin/python3 -m pip install --no-cache-dir "openvino==2025.4.1" "rapidocr-openvino==1.2.3" && \
    /opt/venv/bin/python3 -c "import rapidocr, onnxruntime; print('RapidOCR v3 OK')" && \
    /opt/venv/bin/python3 -c "import openvino, rapidocr_openvino; print('OpenVINO OCR OK')"

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
