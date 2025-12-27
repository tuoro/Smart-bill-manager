#!/usr/bin/env python3
"""
OCR CLI - Command line interface for OCR (RapidOCR v3)
Usage: python paddleocr_cli.py <image_path>
Output: JSON with extracted text
"""

import sys
import json
import os
import contextlib
import argparse
from importlib import metadata


@contextlib.contextmanager
def suppress_child_output():
    """
    Suppress any third-party stdout/stderr noise (e.g. RapidOCR logs) so this CLI
    prints strict JSON only. This is required because the Go backend parses the
    entire process output as JSON.
    """
    devnull = open(os.devnull, "w")
    old_stdout_fd = os.dup(1)
    old_stderr_fd = os.dup(2)
    try:
        os.dup2(devnull.fileno(), 1)
        os.dup2(devnull.fileno(), 2)
        yield
    finally:
        try:
            os.dup2(old_stdout_fd, 1)
            os.dup2(old_stderr_fd, 2)
        finally:
            os.close(old_stdout_fd)
            os.close(old_stderr_fd)
            devnull.close()


def main():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("image_path", nargs="?")
    parser.add_argument("--profile", choices=["default", "pdf"], default="default")
    parser.add_argument("--max-side-len", type=int, default=None)
    parser.add_argument("--min-height", type=int, default=None)
    parser.add_argument("--text-score", type=float, default=None)
    args = parser.parse_args()

    if not args.image_path:
        print(json.dumps({"success": False, "error": "No image path provided"}))
        sys.exit(1)

    image_path = args.image_path
    
    if not os.path.exists(image_path):
        print(json.dumps({"success": False, "error": f"Image file not found: {image_path}"}))
        sys.exit(1)

    # RapidOCR v3 (rapidocr + onnxruntime)
    try:
        with suppress_child_output():
            from rapidocr import RapidOCR

            rapidocr_version = "unknown"
            try:
                rapidocr_version = metadata.version("rapidocr")
            except metadata.PackageNotFoundError:
                pass

            params = {}

            # PDF pages are usually A4-sized images; RapidOCR defaults (e.g. max_side_len=2000, min_height=30)
            # can downscale too aggressively and miss small fields like 发票代码/号码/日期.
            if args.profile == "pdf":
                params.update(
                    {
                        "Global.max_side_len": 4096,
                        "Global.min_height": 10,
                        "Global.text_score": 0.35,
                    }
                )

            if args.max_side_len is not None:
                params["Global.max_side_len"] = int(args.max_side_len)
            if args.min_height is not None:
                params["Global.min_height"] = int(args.min_height)
            if args.text_score is not None:
                params["Global.text_score"] = float(args.text_score)

            ocr = RapidOCR(params=params or None)
            out = ocr(image_path)

        txts = getattr(out, "txts", None) or ()
        scores = getattr(out, "scores", None) or ()
        boxes = getattr(out, "boxes", None)
        if boxes is not None and hasattr(boxes, "tolist"):
            boxes = boxes.tolist()

        lines = []
        full_text_parts = []
        
        for idx, text in enumerate(txts):
            confidence = 0.0
            if idx < len(scores) and scores[idx] is not None:
                confidence = float(scores[idx])

            line = {"text": text, "confidence": confidence}
            if boxes is not None and idx < len(boxes):
                line["box"] = boxes[idx]

            lines.append(line)
            full_text_parts.append(text)
        
        print(json.dumps({
            "success": True,
            "text": "\n".join(full_text_parts),
            "lines": lines,
            "line_count": len(lines),
            "engine": f"rapidocr-{rapidocr_version}",
            "profile": args.profile,
        }, ensure_ascii=False))
        return
        
    except ImportError:
        print(json.dumps({
            "success": False,
            "error": "RapidOCR v3 not available. Install rapidocr and onnxruntime.",
        }, ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}, ensure_ascii=False))
        sys.exit(1)

if __name__ == "__main__":
    main()
