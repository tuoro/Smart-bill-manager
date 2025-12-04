#!/usr/bin/env python3
"""
OCR CLI - Command line interface for OCR (RapidOCR/PaddleOCR)
Usage: python paddleocr_cli.py <image_path>
Output: JSON with extracted text
"""

import sys
import json
import os

def main():
    if len(sys.argv) < 2:
        print(json.dumps({"success": False, "error": "No image path provided"}))
        sys.exit(1)
    
    image_path = sys.argv[1]
    
    if not os.path.exists(image_path):
        print(json.dumps({"success": False, "error": f"Image file not found: {image_path}"}))
        sys.exit(1)
    
    # Try RapidOCR first (lightweight, better compatibility)
    try:
        from rapidocr_onnxruntime import RapidOCR
        ocr = RapidOCR()
        # RapidOCR returns (result, elapsed_time) - we only need result
        result, elapsed_time = ocr(image_path)
        
        lines = []
        full_text_parts = []
        
        if result:
            for item in result:
                text = item[1]
                confidence = item[2]
                lines.append({"text": text, "confidence": confidence})
                full_text_parts.append(text)
        
        print(json.dumps({
            "success": True,
            "text": "\n".join(full_text_parts),
            "lines": lines,
            "line_count": len(lines)
        }, ensure_ascii=False))
        return
        
    except ImportError:
        pass  # Fall back to PaddleOCR
    except (RuntimeError, ValueError, OSError):
        # RapidOCR failed with expected error, try PaddleOCR fallback
        pass
    
    # Fall back to PaddleOCR
    try:
        from paddleocr import PaddleOCR
        
        # Initialize PaddleOCR (lazy loading, will cache after first run)
        # Using 'ch' for Chinese (includes both Traditional and Simplified)
        # For explicit Chinese Simplified only, use 'ch_sim'
        ocr = PaddleOCR(
            use_angle_cls=True,
            lang='ch',
            use_gpu=False,
            show_log=False
        )
        
        # Perform OCR
        result = ocr.ocr(image_path, cls=True)
        
        # Extract text
        lines = []
        full_text_parts = []
        
        if result and result[0]:
            for line in result[0]:
                if line and len(line) >= 2:
                    text = line[1][0]
                    confidence = line[1][1]
                    lines.append({
                        "text": text,
                        "confidence": confidence
                    })
                    full_text_parts.append(text)
        
        output = {
            "success": True,
            "text": "\n".join(full_text_parts),
            "lines": lines,
            "line_count": len(lines)
        }
        print(json.dumps(output, ensure_ascii=False))
        return
        
    except ImportError:
        pass  # No OCR engine available
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}))
        sys.exit(1)
    
    # No OCR engine available
    print(json.dumps({
        "success": False,
        "error": "No OCR engine available. Install rapidocr_onnxruntime or paddleocr"
    }))
    sys.exit(1)

if __name__ == "__main__":
    main()
