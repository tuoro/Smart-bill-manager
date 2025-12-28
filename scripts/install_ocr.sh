#!/bin/bash
# Install OCR dependencies (current default engine: RapidOCR v3 via onnxruntime).

echo "Installing OCR dependencies (RapidOCR v3)..."

# Check if pip is available
if command -v pip3 &> /dev/null; then
    pip3 install "rapidocr==3.*" onnxruntime pillow pymupdf
elif command -v pip &> /dev/null; then
    pip install "rapidocr==3.*" onnxruntime pillow pymupdf
else
    echo "Error: pip not found. Please install Python first."
    exit 1
fi

echo "OCR dependencies installed successfully!"
