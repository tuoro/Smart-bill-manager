#!/bin/bash
# Install RapidOCR v3 dependencies (rapidocr + onnxruntime)

echo "Installing RapidOCR v3 (rapidocr + onnxruntime)..."

# Check if pip is available
if command -v pip3 &> /dev/null; then
    pip3 install "rapidocr==3.*" onnxruntime
elif command -v pip &> /dev/null; then
    pip install "rapidocr==3.*" onnxruntime
else
    echo "Error: pip not found. Please install Python first."
    exit 1
fi

echo "RapidOCR v3 installed successfully!"
