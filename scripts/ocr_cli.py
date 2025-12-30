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
import traceback
import os
import sys
import tempfile
from importlib import metadata
from pathlib import Path


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


def reorder_lines_by_box(lines: list[dict]) -> list[dict]:
    """
    Reorder OCR lines based on bounding boxes:
    - Cluster by Y (row) with a tolerance based on median height
    - Sort rows by top, and items within a row by left
    If boxes are missing, original order is kept.
    """
    if not lines:
        return lines

    processed = []
    for line in lines:
        box = line.get("box") or []
        if not box:
            continue
        try:
            xs = [p[0] for p in box if isinstance(p, (list, tuple)) and len(p) >= 2]
            ys = [p[1] for p in box if isinstance(p, (list, tuple)) and len(p) >= 2]
            if not xs or not ys:
                continue
            minx, maxx = min(xs), max(xs)
            miny, maxy = min(ys), max(ys)
            processed.append(
                {
                    "line": line,
                    "minx": minx,
                    "maxx": maxx,
                    "miny": miny,
                    "maxy": maxy,
                    "height": max(1.0, maxy - miny),
                }
            )
        except Exception:
            continue

    if not processed:
        return lines

    heights = sorted(p["height"] for p in processed)
    mid = len(heights) // 2
    median_h = heights[mid] if len(heights) % 2 else (heights[mid - 1] + heights[mid]) / 2
    row_tol = max(5.0, median_h * 0.6)

    processed.sort(key=lambda p: (p["miny"], p["minx"]))

    rows: list[list[dict]] = []
    current_row: list[dict] = []
    current_maxy = None
    for p in processed:
        if current_row and current_maxy is not None and p["miny"] > current_maxy + row_tol:
            rows.append(current_row)
            current_row = [p]
            current_maxy = p["maxy"]
        else:
            current_row.append(p)
            current_maxy = p["maxy"] if current_maxy is None else max(current_maxy, p["maxy"])
    if current_row:
        rows.append(current_row)

    ordered = []
    for row in rows:
        row.sort(key=lambda p: p["minx"])
        ordered.extend(row)

    return [p["line"] for p in ordered]


def apply_label_value_layout(lines: list[dict], profile: str) -> list[dict]:
    """
    Layout-aware postprocess for "bill detail" screenshots where OCR may output a label column
    and a value column separately. We pair labels with their nearest value using box geometry,
    then rewrite the label line into "标签：值" and remove the paired value line.

    This makes downstream field parsing much more stable (especially with PP-OCRv5).
    """
    if profile != "default" or not lines:
        return lines

    label_set = {
        "交易单号",
        "商户单号",
        "订单号",
        "商品",
        "服务",
        "支付方式",
        "付款方式",
        "当前状态",
        "支付时间",
        "转账时间",
        "商户全称",
        "收单机构",
    }

    def norm(t: str) -> str:
        return str(t or "").strip()

    def is_label(t: str) -> bool:
        return norm(t) in label_set

    def box_of(line: dict):
        box = line.get("box") or []
        if not isinstance(box, (list, tuple)) or not box:
            return None
        try:
            xs = [p[0] for p in box if isinstance(p, (list, tuple)) and len(p) >= 2]
            ys = [p[1] for p in box if isinstance(p, (list, tuple)) and len(p) >= 2]
            if not xs or not ys:
                return None
            left = float(min(xs))
            right = float(max(xs))
            top = float(min(ys))
            bottom = float(max(ys))
            return {
                "left": left,
                "right": right,
                "top": top,
                "bottom": bottom,
                "cx": (left + right) / 2.0,
                "cy": (top + bottom) / 2.0,
                "h": max(bottom - top, 1.0),
                "w": max(right - left, 1.0),
            }
        except Exception:
            return None

    label_idxs = [i for i, ln in enumerate(lines) if is_label(ln.get("text", ""))]
    if len(label_idxs) < 4:
        return lines

    boxes = [box_of(ln) for ln in lines]
    heights = sorted([b["h"] for b in boxes if b])
    if not heights:
        return lines
    median_h = heights[len(heights) // 2]
    y_tol = max(8.0, median_h * 0.75)
    x_tol = max(24.0, median_h * 2.5)

    used_values: set[int] = set()
    label_to_value: dict[int, int] = {}

    for li in label_idxs:
        lb = boxes[li]
        if not lb:
            continue

        best = None
        best_dx = None
        for vi, vb in enumerate(boxes):
            if vi == li or vi in used_values:
                continue
            if is_label(lines[vi].get("text", "")):
                continue
            if not vb:
                continue
            if abs(vb["cy"] - lb["cy"]) > y_tol:
                continue
            dx = vb["left"] - lb["right"]
            if dx < -5.0:
                continue
            if best_dx is None or dx < best_dx:
                best_dx = dx
                best = vi

        if best is None:
            best_dy = None
            for vi, vb in enumerate(boxes):
                if vi == li or vi in used_values:
                    continue
                if is_label(lines[vi].get("text", "")):
                    continue
                if not vb:
                    continue
                if vb["top"] < lb["bottom"] - 2.0:
                    continue
                if abs(vb["left"] - lb["left"]) > x_tol:
                    continue
                dy = vb["top"] - lb["bottom"]
                if best_dy is None or dy < best_dy:
                    best_dy = dy
                    best = vi

        if best is not None:
            label_to_value[li] = best
            used_values.add(best)

    if not label_to_value:
        return lines

    out: list[dict] = []
    for i, ln in enumerate(lines):
        if i in used_values:
            continue
        t = norm(ln.get("text", ""))
        if i in label_to_value:
            v_idx = label_to_value[i]
            v_text = norm(lines[v_idx].get("text", ""))
            if v_text:
                merged = dict(ln)
                merged["text"] = f"{t}：{v_text}"
                try:
                    merged["confidence"] = float(
                        min(
                            float(ln.get("confidence") or 0.0),
                            float(lines[v_idx].get("confidence") or 0.0),
                        )
                    )
                except Exception:
                    pass
                out.append(merged)
                continue
        out.append(ln)
    return out


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
    # 不使用自定义模型路径，直接交给 RapidOCR 内置下载/加载
    return {}


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
            from rapidocr import EngineType, LangDet, LangRec, ModelType, OCRVersion, RapidOCR
            from rapidocr.inference_engine.base import InferSession

            rapidocr_version = "unknown"
            try:
                rapidocr_version = metadata.version("rapidocr")
            except metadata.PackageNotFoundError:
                pass

            params: dict = {}
            # Force PP-OCRv5 by default (per RapidOCR官方文档).
            # If this fails (e.g. first-time model download issues), we fall back to RapidOCR defaults.
            params.update(
                {
                    "Det.engine_type": EngineType.ONNXRUNTIME,
                    "Det.lang_type": LangDet.CH,
                    "Det.model_type": ModelType.MOBILE,
                    "Det.ocr_version": OCRVersion.PPOCRV5,
                    "Rec.engine_type": EngineType.ONNXRUNTIME,
                    "Rec.lang_type": LangRec.CH,
                    "Rec.model_type": ModelType.MOBILE,
                    "Rec.ocr_version": OCRVersion.PPOCRV5,
                }
            )

            # Optional: override RapidOCR's default model cache directory with a single mounted dir.
            # By default RapidOCR stores models under the Python package directory (rapidocr/models).
            model_data_dir = (os.getenv("SBM_OCR_DATA_DIR") or os.getenv("SBM_DATA_DIR") or "").strip()
            if model_data_dir:
                try:
                    base = Path(model_data_dir).expanduser()
                    # Keep backward/UX-friendly behavior:
                    # - If user points SBM_OCR_DATA_DIR to a parent dir, we create/use <dir>/rapidocr-models
                    # - If user already points to .../rapidocr-models, use it directly (avoid double nesting)
                    if base.name != "rapidocr-models":
                        base = base / "rapidocr-models"
                    if not base.is_absolute():
                        base = (Path.cwd() / base).resolve()
                    base.mkdir(parents=True, exist_ok=True)
                    InferSession.DEFAULT_MODEL_PATH = base
                except Exception:
                    model_data_dir = ""

            if args.profile == "pdf":
                params.update(
                    {
                        "Global.max_side_len": 4096,
                        "Global.min_height": 10,
                        "Global.text_score": 0.35,
                    }
                )
            elif args.profile == "default":
                # 单次 OCR 提升长行召回（不做二次识别）：
                # - 放宽宽高比限制，减少长行被过度压缩/截断
                # - 增大 Rec 输入宽度，降低“扫二维码付款-给XXX”这类长行漏字概率
                params.update(
                    {
                        "Global.width_height_ratio": 15,
                        "Rec.rec_img_shape": [3, 48, 640],
                    }
                )

            if args.max_side_len is not None:
                params["Global.max_side_len"] = int(args.max_side_len)
            if args.min_height is not None:
                params["Global.min_height"] = int(args.min_height)
            if args.text_score is not None:
                params["Global.text_score"] = float(args.text_score)

            # Helper: run rapidocr with optional fallback to default params to avoid crashes such as
            # "list index out of range" from corrupted/partial models.
            def run_with_fallback(path: str):
                errors: list[str] = []
                param_summary = {
                    "rapidocr": rapidocr_version,
                    "ocr_version": "PP-OCRv5",
                    "det": "onnxruntime:PP-OCRv5:ch:mobile",
                    "rec": "onnxruntime:PP-OCRv5:ch:mobile",
                    "cls": "default",
                    "dict": "auto",
                    "width_height_ratio": params.get("Global.width_height_ratio", "default"),
                    "rec_img_shape": params.get("Rec.rec_img_shape", "default"),
                    "model_dir": str(InferSession.DEFAULT_MODEL_PATH) if model_data_dir else "",
                }
                # First try with forced PP-OCRv5 params
                try:
                    ocr = RapidOCR(params=params or None)
                    out = ocr(path)
                    return out, "custom", errors, param_summary
                except Exception as e:
                    errors.append(f"custom_params_failed: {e}; params={param_summary}")
                    # Fallback to RapidOCR defaults (letting RapidOCR auto-manage models)
                    try:
                        ocr = RapidOCR()
                        out = ocr(path)
                        fb_summary = dict(param_summary)
                        fb_summary["ocr_version"] = "default"
                        fb_summary["det"] = "default"
                        fb_summary["rec"] = "default"
                        fb_summary["cls"] = "default"
                        fb_summary["dict"] = "default"
                        return out, "fallback_default", errors, fb_summary
                    except Exception as e2:
                        errors.append(f"default_failed: {e2}")
                        raise RuntimeError("; ".join(errors))

            def build_result(out, backend: str, backend_errors: list[str], param_summary: dict):
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

                ordered = reorder_lines_by_box(lines)
                ordered = apply_label_value_layout(ordered, args.profile)
                for ln in ordered:
                    full_text_parts.append(ln.get("text") or "")

                return {
                    "text": "\n".join(full_text_parts),
                    "lines": ordered,
                    "line_count": len(ordered),
                    "score": score_lines(ordered),
                    "backend": backend,
                    "backend_errors": backend_errors,
                    "params": param_summary,
                }

            def run_one(path: str):
                out, backend, backend_errors, param_summary = run_with_fallback(path)
                return build_result(out, backend, backend_errors, param_summary)

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
                                {
                                    "variant": name,
                                    "score": r["score"],
                                    "lines": r["line_count"],
                                    "chars": len(r["text"]),
                                    "backend": r.get("backend", ""),
                                    "backend_errors": r.get("backend_errors", []),
                                }
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
                "backend": best.get("backend", ""),
                "backend_errors": best.get("backend_errors", []),
                "params": best.get("params", {}),
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
        tb = traceback.format_exc()
        print(json.dumps({"success": False, "error": str(e), "traceback": tb}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()
