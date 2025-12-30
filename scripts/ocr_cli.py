#!/usr/bin/env python3
"""
OCR CLI - Command line interface for OCR (RapidOCR v3, CPU via onnxruntime)
Usage: python ocr_cli.py <image_path>
Output: JSON with extracted text

Notes:
- This CLI must print strict JSON only (the Go backend parses process output).
- For invoice PDF pages (profile=pdf), an optional multi-pass preprocessing is used
  to improve recall of small fields (buyer/seller, invoice code/number/date).
"""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import sys
import tempfile
from importlib import metadata
from pathlib import Path
import hashlib
from urllib import error as urlerror
from urllib import request as urlrequest


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


def truthy(v: str | None) -> bool:
    if v is None:
        return False
    return str(v).strip().lower() in ("1", "true", "yes", "y", "on")


def verify_sha256(path: Path, expected: str) -> bool:
    try:
        h = hashlib.sha256()
        with path.open("rb") as f:
            for chunk in iter(lambda: f.read(8192), b""):
                h.update(chunk)
        return h.hexdigest().lower() == expected.lower()
    except Exception:
        return False


def safe_int(v: str | None, default: int) -> int:
    try:
        return int(str(v).strip())
    except Exception:
        return default


def score_lines(lines: list[dict]) -> float:
    """
    Heuristic to pick the best OCR pass.
    Prioritizes: more content, higher confidence, more CJK/digits.
    """
    if not lines:
        return 0.0

    total_conf = 0.0
    total_len = 0
    cjk = 0
    digits = 0
    for l in lines:
        t = l.get("text") or ""
        total_len += len(t)
        total_conf += float(l.get("confidence") or 0.0)
        for ch in t:
            o = ord(ch)
            if 0x4E00 <= o <= 0x9FFF:
                cjk += 1
            elif "0" <= ch <= "9":
                digits += 1

    avg_conf = total_conf / max(len(lines), 1)
    content = (cjk * 2.0) + (digits * 1.2) + (max(total_len - cjk - digits, 0) * 0.25)
    return (content + (len(lines) * 10.0)) * (0.25 + avg_conf)


def load_pil_image(path: str):
    try:
        from PIL import Image

        im = Image.open(path)
        im.load()
        return im
    except Exception:
        return None


def build_variants(pil_img, profile: str, multipass: int, rotate180: bool):
    if pil_img is None or multipass <= 0:
        return []

    try:
        from PIL import ImageEnhance, ImageOps
    except Exception:
        return []

    variants = []
    base = pil_img.convert("RGB")

    if profile == "pdf":
        w, h = base.size
        scale = 2

        up = base.resize((w * scale, h * scale))
        up = ImageOps.autocontrast(up)
        up = ImageEnhance.Contrast(up).enhance(1.15)
        up = ImageEnhance.Sharpness(up).enhance(1.6)
        variants.append(("enhance2x", up))

        if multipass >= 2:
            gray = base.convert("L")
            gray = ImageOps.autocontrast(gray)
            gray = gray.resize((w * scale, h * scale))
            variants.append(("gray2x", gray.convert("RGB")))

        if rotate180:
            variants.append(("enhance2x_rot180", up.rotate(180, expand=True)))

    return variants


def resolve_model_paths():
    """
    Resolve model paths for RapidOCR with PP-OCRv5 defaults.
    - Base directory: SBM_RAPIDOCR_MODEL_DIR (default: /opt/rapidocr-models)
    - Explicit overrides (highest优先级):
        SBM_RAPIDOCR_DET_MODEL / SBM_RAPIDOCR_REC_MODEL / SBM_RAPIDOCR_CLS_MODEL
    - Otherwise：自动检测并若缺失则下载 PP-OCRv5 onnx 模型到 base_dir
    - 如果下载失败，会回退到 RapidOCR 内置默认模型（RapidOCR 自行下载）
    """

    base_dir = os.getenv("SBM_RAPIDOCR_MODEL_DIR", "/opt/rapidocr-models")
    Path(base_dir).mkdir(parents=True, exist_ok=True)

    # 参考 RapidOCR default_models.yaml（ONNX，mobile 版本，v3.5.0）
    models = {
        "det": {
            "env": "SBM_RAPIDOCR_DET_MODEL",
            "filename": "ch_PP-OCRv5_mobile_det.onnx",
            "url": "https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.5.0/onnx/PP-OCRv5/det/ch_PP-OCRv5_mobile_det.onnx",
            "sha256": "4d97c44a20d30a81aad087d6a396b08f786c4635742afc391f6621f5c6ae78ae",
        },
        "rec": {
            "env": "SBM_RAPIDOCR_REC_MODEL",
            "filename": "ch_PP-OCRv5_rec_mobile_infer.onnx",
            "url": "https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.5.0/onnx/PP-OCRv5/rec/ch_PP-OCRv5_rec_mobile_infer.onnx",
            "sha256": "5825fc7ebf84ae7a412be049820b4d86d77620f204a041697b0494669b1742c5",
        },
        "dict": {
            "env": "SBM_RAPIDOCR_DICT_PATH",
            "filename": "ppocrv5_dict.txt",
            "url": "https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.5.0/paddle/PP-OCRv5/rec/ch_PP-OCRv5_rec_mobile_infer/ppocrv5_dict.txt",
            # 字典文件不做校验
            "sha256": None,
        },
        "cls": {
            "env": "SBM_RAPIDOCR_CLS_MODEL",
            "filename": "ch_ppocr_mobile_v2.0_cls_infer.onnx",
            "url": "https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.5.0/onnx/PP-OCRv4/cls/ch_ppocr_mobile_v2.0_cls_infer.onnx",
            "sha256": "e47acedf663230f8863ff1ab0e64dd2d82b838fceb5957146dab185a89d6215c",
        },
    }

    params: dict[str, str] = {}

    def ensure_model(key: str):
        cfg = models[key]
        env_path = os.getenv(cfg["env"])
        if env_path and os.path.isfile(env_path):
            # Even env override must pass hash check (if provided)
            if cfg.get("sha256"):
                if verify_sha256(Path(env_path), cfg["sha256"]):
                    return env_path
            else:
                return env_path

        local_path = Path(base_dir) / cfg["filename"]
        # If file exists, optionally verify hash
        if local_path.is_file():
            if cfg.get("sha256"):
                if verify_sha256(local_path, cfg["sha256"]):
                    return str(local_path)
                else:
                    try:
                        local_path.unlink()
                    except OSError:
                        pass
            else:
                return str(local_path)

        # Try download if not present or failed hash
        try:
            urlrequest.urlretrieve(cfg["url"], local_path)
            if cfg.get("sha256") and not verify_sha256(local_path, cfg["sha256"]):
                try:
                    local_path.unlink()
                except OSError:
                    pass
                return None
            return str(local_path)
        except (urlerror.URLError, OSError):
            return None

    det = ensure_model("det")
    rec = ensure_model("rec")
    rec_dict = ensure_model("dict")
    cls = ensure_model("cls")

    if det:
        params["det_model_path"] = det
    if rec:
        params["rec_model_path"] = rec
    if rec_dict:
        params["rec_char_dict_path"] = rec_dict
    if cls:
        params["cls_model_path"] = cls
    return params


def main():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("image_path", nargs="?")
    parser.add_argument("--engine", choices=["rapidocr"], default=None)
    parser.add_argument("--profile", choices=["default", "pdf"], default="default")
    parser.add_argument("--max-side-len", type=int, default=None)
    parser.add_argument("--min-height", type=int, default=None)
    parser.add_argument("--text-score", type=float, default=None)
    parser.add_argument("--debug", action="store_true")
    args = parser.parse_args()

    if not args.image_path:
        print(json.dumps({"success": False, "error": "No image path provided"}))
        sys.exit(1)

    image_path = args.image_path
    if not os.path.exists(image_path):
        print(json.dumps({"success": False, "error": f"Image file not found: {image_path}"}))
        sys.exit(1)

    engine = args.engine or os.getenv("SBM_OCR_ENGINE") or "rapidocr"
    engine = str(engine).strip().lower()
    # Be tolerant to old configs (e.g. SBM_OCR_ENGINE=openvino) and fall back to rapidocr.
    if engine != "rapidocr":
        engine = "rapidocr"

    # RapidOCR v3 (rapidocr + onnxruntime)
    try:
        with suppress_child_output():
            from rapidocr import RapidOCR

            rapidocr_version = "unknown"
            try:
                rapidocr_version = metadata.version("rapidocr")
            except metadata.PackageNotFoundError:
                pass

            params: dict = {}
            # Preferred PP-OCRv5 weights if present; falls back to RapidOCR defaults otherwise.
            params.update(resolve_model_paths())
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

            def run_one(path: str):
                out = ocr(path)
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

                return {
                    "text": "\n".join(full_text_parts),
                    "lines": lines,
                    "line_count": len(lines),
                    "score": score_lines(lines),
                }

            multipass = safe_int(os.getenv("SBM_RAPIDOCR_MULTIPASS"), 1 if args.profile == "pdf" else 0)
            rotate180 = truthy(os.getenv("SBM_RAPIDOCR_ROTATE180")) or (args.profile == "pdf")
            debug = args.debug or truthy(os.getenv("SBM_OCR_DEBUG"))

            best = run_one(image_path)
            best_variant = "original"
            variants_debug = []
            if debug:
                variants_debug.append(
                    {"variant": best_variant, "score": best["score"], "lines": best["line_count"], "chars": len(best["text"])}
                )

            pil_img = load_pil_image(image_path) if multipass > 0 else None
            variants = build_variants(pil_img, args.profile, multipass, rotate180)

            if variants:
                with tempfile.TemporaryDirectory(prefix="sbm-ocr-") as td:
                    for name, im in variants:
                        out_path = str(Path(td) / f"{name}.png")
                        try:
                            im.save(out_path, format="PNG", optimize=True)
                        except Exception:
                            continue

                        r = run_one(out_path)
                        if debug:
                            variants_debug.append(
                                {"variant": name, "score": r["score"], "lines": r["line_count"], "chars": len(r["text"])}
                            )
                        if r["score"] > best["score"]:
                            best = r
                            best_variant = name

        payload = {
            "success": True,
            "text": best["text"],
            "lines": best["lines"],
            "line_count": best["line_count"],
            "engine": f"rapidocr-{rapidocr_version}",
            "profile": args.profile,
            "variant": best_variant,
        }
        if debug:
            payload["variants"] = variants_debug

        print(json.dumps(payload, ensure_ascii=False))
        return

    except ImportError:
        print(
            json.dumps(
                {
                    "success": False,
                    "error": "RapidOCR v3 not available. Install rapidocr and onnxruntime.",
                },
                ensure_ascii=False,
            )
        )
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()
