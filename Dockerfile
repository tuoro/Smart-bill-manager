# Unified Dockerfile for Smart Bill Manager
# This builds both frontend and backend into a single image

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
# Stage 2: Build Backend
# ============================================
FROM node:24-alpine AS backend-builder

WORKDIR /app/backend

# Copy backend package files
COPY backend/package*.json ./

# Install all dependencies (including devDependencies for build)
RUN npm ci

# Copy backend source code
COPY backend/ .

# Build TypeScript (use npx to ensure local typescript is found)
RUN npx tsc

# ============================================
# Stage 3: Production Image
# ============================================
FROM nginx:alpine AS production

# Install Node.js and supervisor
RUN apk add --no-cache nodejs npm supervisor

WORKDIR /app

# Copy backend package files and install production dependencies
COPY backend/package*.json ./backend/

# Install build dependencies for better-sqlite3, install production deps, then clean up
RUN cd backend && \
    apk add --no-cache --virtual .build-deps python3 make g++ && \
    npm ci --omit=dev && \
    apk del .build-deps

# Copy built backend files
COPY --from=backend-builder /app/backend/dist ./backend/dist

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

# Start supervisord which manages both nginx and node
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]
