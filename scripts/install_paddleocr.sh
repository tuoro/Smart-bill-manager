#!/bin/bash
# Install OCR dependencies:
# - RapidOCR v3 (rapidocr + onnxruntime)
# - OpenVINO OCR (openvino + rapidocr-openvino)

echo "Installing OCR dependencies (RapidOCR v3 + OpenVINO OCR)..."

# Check if pip is available
if command -v pip3 &> /dev/null; then
    pip3 install "rapidocr==3.*" onnxruntime
    pip3 install "openvino==2025.4.1" "rapidocr-openvino==1.2.3"
elif command -v pip &> /dev/null; then
    pip install "rapidocr==3.*" onnxruntime
    pip install "openvino==2025.4.1" "rapidocr-openvino==1.2.3"
else
    echo "Error: pip not found. Please install Python first."
    exit 1
fi

echo "OCR dependencies installed successfully!"
