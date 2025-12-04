# PaddleOCR Service

A Flask-based HTTP service that provides OCR (Optical Character Recognition) capabilities using PaddleOCR.

## Features

- **Chinese + English OCR**: Supports recognition of both Chinese and English text
- **High Accuracy**: Uses PaddleOCR, optimized for Chinese payment screenshots
- **Text Angle Classification**: Automatically handles rotated text
- **RESTful API**: Simple HTTP endpoints for easy integration
- **Docker Support**: Containerized deployment

## API Endpoints

### Health Check

```
GET /health
```

Returns the service status.

**Response:**
```json
{
  "status": "ok",
  "service": "paddleocr"
}
```

### OCR by File Upload

```
POST /ocr
Content-Type: multipart/form-data
```

Upload an image file for OCR recognition.

**Request:**
- Form field `image`: The image file to process

**Response:**
```json
{
  "success": true,
  "text": "Extracted text with line breaks",
  "lines": [
    {
      "text": "Line text",
      "confidence": 0.95,
      "box": [[x1, y1], [x2, y2], [x3, y3], [x4, y4]]
    }
  ],
  "line_count": 10
}
```

### OCR by File Path

```
POST /ocr/path
Content-Type: application/json
```

Process an image file from the filesystem (useful when the service shares volumes with other containers).

**Request:**
```json
{
  "image_path": "/path/to/image.png"
}
```

**Response:**
```json
{
  "success": true,
  "text": "Extracted text with line breaks",
  "lines": [...],
  "line_count": 10
}
```

## Environment Variables

- `PADDLEOCR_HOST`: Host to bind to (default: `0.0.0.0`)
- `PADDLEOCR_PORT`: Port to listen on (default: `5000`)
- `PADDLEOCR_DEBUG`: Enable Flask debug mode (default: `false`)

## Deployment

### Docker Compose (Recommended)

The service is integrated into the main `docker-compose.yml`:

```yaml
services:
  paddleocr:
    build:
      context: ./paddleocr-service
      dockerfile: Dockerfile
    container_name: paddleocr-service
    ports:
      - "5000:5000"
    volumes:
      - ./uploads:/app/uploads:ro
    environment:
      - PADDLEOCR_HOST=0.0.0.0
      - PADDLEOCR_PORT=5000
    restart: unless-stopped
```

### Standalone Development

For local development without Docker:

```bash
# Run the start script
chmod +x start.sh
./start.sh
```

The script will:
1. Create a Python virtual environment (if needed)
2. Install dependencies
3. Start the Flask service on `http://localhost:5000`

## Testing

Test the service using curl:

```bash
# Health check
curl http://localhost:5000/health

# OCR by file upload
curl -X POST -F "image=@test.png" http://localhost:5000/ocr

# OCR by file path
curl -X POST -H "Content-Type: application/json" \
  -d '{"image_path": "/app/uploads/test.png"}' \
  http://localhost:5000/ocr/path
```

## Integration with Go Backend

The Go backend automatically detects and uses PaddleOCR when available:

1. **Primary**: Tries PaddleOCR for best recognition quality
2. **Fallback**: Uses Tesseract if PaddleOCR is unavailable

Configure the Go backend to use this service:

```bash
export PADDLEOCR_URL="http://localhost:5000"
```

Or in Docker Compose:

```yaml
backend:
  environment:
    - PADDLEOCR_URL=http://paddleocr:5000
```

## Performance

- **First request**: ~2-3 seconds (model loading)
- **Subsequent requests**: ~500ms-1s per image
- **Memory usage**: ~1-2GB RAM
- **CPU usage**: Works well on CPU-only systems

## Troubleshooting

### Service won't start

- Check Python version (requires Python 3.7+)
- Verify all system dependencies are installed
- Check port 5000 is not already in use

### Poor OCR accuracy

- Ensure images are clear and high resolution
- Check image orientation (PaddleOCR handles rotation automatically)
- Verify the `lang` parameter matches your content language

### High memory usage

- PaddleOCR models are loaded into memory on startup
- Consider using GPU acceleration for better performance
- Adjust container memory limits if needed
