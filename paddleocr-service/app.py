#!/usr/bin/env python3
"""
PaddleOCR Service - A Flask-based OCR service using PaddleOCR
Provides HTTP API for text extraction from images
"""

import os
import json
import tempfile
import traceback
from flask import Flask, request, jsonify
from paddleocr import PaddleOCR

app = Flask(__name__)

# Initialize PaddleOCR with Chinese + English support
# use_angle_cls=True enables text angle classification
# use_gpu=False for CPU-only deployment (set True if GPU available)
ocr = PaddleOCR(
    use_angle_cls=True,
    lang='ch',  # Chinese (includes English)
    use_gpu=False,
    show_log=False
)

def process_ocr_result(result):
    """
    Helper function to process OCR results and extract text
    
    Args:
        result: PaddleOCR result object
        
    Returns:
        dict: Processed OCR results with text and line information
    """
    extracted_lines = []
    full_text_parts = []
    
    if result and result[0]:
        for line in result[0]:
            if line and len(line) >= 2:
                box = line[0]  # Bounding box coordinates
                text_info = line[1]  # (text, confidence)
                text = text_info[0]
                confidence = text_info[1]
                
                extracted_lines.append({
                    'text': text,
                    'confidence': confidence,
                    'box': box
                })
                full_text_parts.append(text)
    
    full_text = '\n'.join(full_text_parts)
    
    return {
        'success': True,
        'text': full_text,
        'lines': extracted_lines,
        'line_count': len(extracted_lines)
    }

@app.route('/health', methods=['GET'])
def health():
    """Health check endpoint"""
    return jsonify({'status': 'ok', 'service': 'paddleocr'})

@app.route('/ocr', methods=['POST'])
def recognize():
    """
    OCR endpoint - accepts image file and returns extracted text
    
    Request: multipart/form-data with 'image' file
    Response: JSON with extracted text and detailed results
    """
    if 'image' not in request.files:
        return jsonify({'error': 'No image file provided'}), 400
    
    image_file = request.files['image']
    
    if image_file.filename == '':
        return jsonify({'error': 'Empty filename'}), 400
    
    tmp_path = None
    try:
        # Save uploaded file to temp location
        with tempfile.NamedTemporaryFile(delete=False, suffix='.png') as tmp:
            image_file.save(tmp.name)
            tmp_path = tmp.name
        
        # Perform OCR
        result = ocr.ocr(tmp_path, cls=True)
        
        # Process and return results
        return jsonify(process_ocr_result(result))
        
    except Exception as e:
        # Log full exception for debugging
        print(f"OCR error (file upload): {str(e)}")
        print(traceback.format_exc())
        
        return jsonify({
            'success': False,
            'error': 'OCR processing failed'
        }), 500
        
    finally:
        # Ensure temp file is cleaned up even if error occurs
        if tmp_path and os.path.exists(tmp_path):
            try:
                os.unlink(tmp_path)
            except Exception as e:
                print(f"Failed to clean up temp file: {e}")

@app.route('/ocr/path', methods=['POST'])
def recognize_by_path():
    """
    OCR endpoint - accepts image path and returns extracted text
    
    Request: JSON with 'image_path' field
    Response: JSON with extracted text and detailed results
    """
    data = request.get_json()
    
    if not data or 'image_path' not in data:
        return jsonify({'error': 'No image_path provided'}), 400
    
    image_path = data['image_path']
    
    if not os.path.exists(image_path):
        return jsonify({'error': 'Image file not found'}), 404
    
    try:
        # Perform OCR
        result = ocr.ocr(image_path, cls=True)
        
        # Process and return results
        return jsonify(process_ocr_result(result))
        
    except Exception as e:
        # Log full exception for debugging
        print(f"OCR error (file path): {str(e)}")
        print(traceback.format_exc())
        
        return jsonify({
            'success': False,
            'error': 'OCR processing failed'
        }), 500

if __name__ == '__main__':
    port = int(os.environ.get('PADDLEOCR_PORT', 5000))
    host = os.environ.get('PADDLEOCR_HOST', '0.0.0.0')
    debug = os.environ.get('PADDLEOCR_DEBUG', 'false').lower() == 'true'
    
    print(f"Starting PaddleOCR service on {host}:{port}")
    app.run(host=host, port=port, debug=debug)

