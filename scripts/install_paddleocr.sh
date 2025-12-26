#!/bin/bash
# Install RapidOCR dependencies (RapidOCR only)

echo "Installing RapidOCR (rapidocr_onnxruntime)..."

# Check if pip is available
if command -v pip3 &> /dev/null; then
    pip3 install rapidocr_onnxruntime
elif command -v pip &> /dev/null; then
    pip install rapidocr_onnxruntime
else
    echo "Error: pip not found. Please install Python first."
    exit 1
fi

echo "RapidOCR installed successfully!"
