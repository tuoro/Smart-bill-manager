#!/usr/bin/env python3
"""
PDF Text Extract CLI (PyMuPDF)

Usage:
  python pdf_text_cli.py <pdf_path> [--layout zones|ordered|raw]

Outputs strict JSON only (required by Go backend parsing).
"""

from __future__ import annotations

import contextlib
import json
import os
import sys
from importlib import metadata


@contextlib.contextmanager
def suppress_child_output():
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
    if len(sys.argv) < 2 or not sys.argv[1]:
        print(json.dumps({"success": False, "error": "No PDF path provided"}))
        sys.exit(1)

    pdf_path = sys.argv[1]
    if not os.path.exists(pdf_path):
        print(json.dumps({"success": False, "error": f"PDF file not found: {pdf_path}"}))
        sys.exit(1)

    try:
        with suppress_child_output():
            import fitz  # PyMuPDF

            pymupdf_version = "unknown"
            try:
                pymupdf_version = metadata.version("pymupdf")
            except metadata.PackageNotFoundError:
                try:
                    pymupdf_version = metadata.version("PyMuPDF")
                except metadata.PackageNotFoundError:
                    pass

            doc = fitz.open(pdf_path)
            raw_parts = []
            ordered_parts = []
            zoned_parts = []
            zoned_pages = []
            page_count = 0

            layout_mode = "zones"
            if "--layout" in sys.argv:
                try:
                    layout_mode = sys.argv[sys.argv.index("--layout") + 1].strip().lower()
                except Exception:
                    layout_mode = "zones"
            if layout_mode not in ("zones", "ordered", "raw"):
                layout_mode = "zones"

            include_zones = True
            if "--include-zones" in sys.argv:
                try:
                    v = sys.argv[sys.argv.index("--include-zones") + 1].strip().lower()
                    include_zones = v not in ("0", "false", "no", "off")
                except Exception:
                    include_zones = True
            # Default: include first page zones only to keep payload small.
            max_zones_pages = 1
            if "--zones-pages" in sys.argv:
                try:
                    max_zones_pages = int(sys.argv[sys.argv.index("--zones-pages") + 1])
                except Exception:
                    max_zones_pages = 1
            if max_zones_pages < 0:
                max_zones_pages = 0

            def iter_page_spans(page):
                try:
                    d = page.get_text("dict") or {}
                except Exception:
                    return []
                out = []
                for b in d.get("blocks", []) or []:
                    if not isinstance(b, dict):
                        continue
                    for ln in b.get("lines", []) or []:
                        if not isinstance(ln, dict):
                            continue
                        bbox = ln.get("bbox")
                        if not bbox or len(bbox) < 4:
                            continue
                        for s in ln.get("spans", []) or []:
                            if not isinstance(s, dict):
                                continue
                            sb = s.get("bbox")
                            if not sb or len(sb) < 4:
                                continue
                            txt = str(s.get("text", "") or "").replace("\r", "\n").strip()
                            if not txt:
                                continue
                            x0, y0, x1, y1 = sb[0], sb[1], sb[2], sb[3]
                            try:
                                h = float(y1) - float(y0)
                            except Exception:
                                h = 0.0
                            out.append(
                                {
                                    "x0": float(x0),
                                    "y0": float(y0),
                                    "x1": float(x1),
                                    "y1": float(y1),
                                    "h": max(h, 1.0),
                                    "t": txt,
                                }
                            )
                return out

            def cluster_rows(items):
                if not items:
                    return []
                items.sort(key=lambda it: (it["y0"], it["x0"]))
                hs = sorted([it["h"] for it in items])
                mid = len(hs) // 2
                median_h = hs[mid] if len(hs) % 2 else (hs[mid - 1] + hs[mid]) / 2.0
                row_tol = max(6.0, median_h * 0.7)

                rows = []
                current = []
                current_max_y = None
                for it in items:
                    if current and current_max_y is not None and it["y0"] > current_max_y + row_tol:
                        rows.append(current)
                        current = [it]
                        current_max_y = it["y1"]
                    else:
                        current.append(it)
                        current_max_y = it["y1"] if current_max_y is None else max(current_max_y, it["y1"])
                if current:
                    rows.append(current)
                return rows

            def region_for(cx, cy):
                # Heuristic zones for A4 Chinese VAT invoices.
                if cy < 0.22:
                    return "header_right" if cx >= 0.55 else "header_left"
                # Password area is a right-side block that extends longer than buyer info.
                if cx >= 0.58 and cy < 0.56:
                    return "password"
                # Buyer block (left) is usually above the items table.
                if cy < 0.40:
                    return "buyer"
                # Items table occupies the middle section.
                if cy < 0.72:
                    return "items"
                if cy < 0.93:
                    return "seller" if cx < 0.58 else "remarks"
                return "footer"

            def build_zoned_text(page, page_no):
                w = float(page.rect.width or 1.0)
                h = float(page.rect.height or 1.0)
                spans = iter_page_spans(page)
                if not spans:
                    return "", None

                buckets = {
                    "header_left": [],
                    "header_right": [],
                    "buyer": [],
                    "password": [],
                    "items": [],
                    "seller": [],
                    "remarks": [],
                    "footer": [],
                }
                for it in spans:
                    cx = ((it["x0"] + it["x1"]) / 2.0) / w
                    cy = ((it["y0"] + it["y1"]) / 2.0) / h
                    buckets[region_for(cx, cy)].append(it)

                def rows_to_lines(items):
                    out = []
                    for row in cluster_rows(items):
                        row.sort(key=lambda it: it["x0"])
                        parts = []
                        for it in row:
                            t = str(it["t"] or "").replace("\n", " ").strip()
                            if t:
                                parts.append(t)
                        if parts:
                            out.append(" ".join(parts))
                    return out

                def rows_payload(region, items):
                    out = []
                    for row in cluster_rows(items):
                        row.sort(key=lambda it: it["x0"])
                        spans2 = []
                        parts = []
                        y0s = []
                        y1s = []
                        for it in row:
                            t = str(it["t"] or "").replace("\n", " ").strip()
                            if not t:
                                continue
                            spans2.append(
                                {
                                    "x0": float(it["x0"]),
                                    "y0": float(it["y0"]),
                                    "x1": float(it["x1"]),
                                    "y1": float(it["y1"]),
                                    "t": t,
                                }
                            )
                            parts.append(t)
                            y0s.append(float(it["y0"]))
                            y1s.append(float(it["y1"]))
                        if spans2:
                            out.append(
                                {
                                    "region": region,
                                    "y0": min(y0s) if y0s else 0.0,
                                    "y1": max(y1s) if y1s else 0.0,
                                    "text": " ".join(parts),
                                    "spans": spans2,
                                }
                            )
                    return out

                hdr = rows_to_lines(buckets["header_right"]) + rows_to_lines(buckets["header_left"])
                buyer = rows_to_lines(buckets["buyer"])
                pwd = rows_to_lines(buckets["password"])
                items = rows_to_lines(buckets["items"])
                seller = rows_to_lines(buckets["seller"])
                other = rows_to_lines(buckets["remarks"]) + rows_to_lines(buckets["footer"])

                sections = [
                    ("发票信息", hdr),
                    ("购买方", buyer),
                    ("密码区", pwd),
                    ("明细", items),
                    ("销售方", seller),
                    ("备注/其他", other),
                ]

                out = [f"【第{page_no}页-分区】"]
                for title, lines2 in sections:
                    if not lines2:
                        continue
                    out.append(f"【{title}】")
                    out.extend(lines2)
                    out.append("")
                zone_payload = None
                if include_zones:
                    rows = []
                    for region, items2 in buckets.items():
                        rows.extend(rows_payload(region, items2))
                    zone_payload = {
                        "page": int(page_no),
                        "width": float(w),
                        "height": float(h),
                        "rows": rows,
                    }
                return "\n".join(out).rstrip(), zone_payload

            for page in doc:
                page_count += 1
                try:
                    raw_parts.append(page.get_text("text") or "")
                except Exception:
                    raw_parts.append("")
                try:
                    if layout_mode == "zones":
                        zoned, zones_meta = build_zoned_text(page, page_count)
                        if zoned:
                            zoned_parts.append(zoned)
                        if (
                            include_zones
                            and zones_meta
                            and max_zones_pages > 0
                            and len(zoned_pages) < max_zones_pages
                        ):
                            zoned_pages.append(zones_meta)
                except Exception:
                    pass
                # Blocks with position, clustered by rows (dynamic tolerance) then sorted by x.
                try:
                    blocks = page.get_text("blocks") or []
                    items = []
                    for b in blocks:
                        if not isinstance(b, (list, tuple)) or len(b) < 5:
                            continue
                        x0, y0, x1, y1, txt = b[0], b[1], b[2], b[3], b[4]
                        try:
                            txt = str(txt or "").replace("\r", "\n").strip()
                        except Exception:
                            txt = ""
                        if not txt:
                            continue
                        try:
                            h = float(y1) - float(y0)
                        except Exception:
                            h = 0.0
                        items.append(
                            {
                                "x0": float(x0),
                                "y0": float(y0),
                                "x1": float(x1),
                                "y1": float(y1),
                                "h": max(h, 1.0),
                                "t": txt,
                            }
                        )

                    if not items:
                        continue

                    items.sort(key=lambda it: (it["y0"], it["x0"]))
                    hs = sorted([it["h"] for it in items])
                    mid = len(hs) // 2
                    median_h = hs[mid] if len(hs) % 2 else (hs[mid - 1] + hs[mid]) / 2.0
                    row_tol = max(6.0, median_h * 0.7)

                    rows = []
                    current = []
                    current_max_y = None
                    for it in items:
                        if current and current_max_y is not None and it["y0"] > current_max_y + row_tol:
                            rows.append(current)
                            current = [it]
                            current_max_y = it["y1"]
                        else:
                            current.append(it)
                            current_max_y = it["y1"] if current_max_y is None else max(current_max_y, it["y1"])
                    if current:
                        rows.append(current)

                    for row in rows:
                        row.sort(key=lambda it: it["x0"])
                        parts = []
                        for it in row:
                            t = it["t"].strip()
                            if t:
                                parts.append(t)
                        if parts:
                            ordered_parts.append(" ".join(parts))
                except Exception:
                    # ignore this page ordering failure
                    continue

            doc.close()

        raw_text = "\n".join(t for t in raw_parts if t)
        ordered_text = "\n".join(ordered_parts)
        zoned_text = "\n\n".join(zoned_parts).strip()

        layout_used = "raw"
        final_text = raw_text
        if layout_mode == "zones" and zoned_text:
            layout_used = "zones"
            final_text = zoned_text
        elif layout_mode in ("zones", "ordered") and ordered_text:
            layout_used = "ordered"
            final_text = ordered_text
        elif raw_text:
            layout_used = "raw"
            final_text = raw_text

        print(
            json.dumps(
                {
                    "success": True,
                    "text": final_text,
                    "raw_text": raw_text,
                    "zoned_text": zoned_text,
                    "zones": zoned_pages if include_zones and zoned_pages else None,
                    "layout": layout_used,
                    "ordered": bool(ordered_text),
                    "page_count": page_count,
                    "extractor": f"pymupdf-{pymupdf_version}",
                },
                ensure_ascii=False,
            )
        )
        return
    except ImportError:
        print(
            json.dumps(
                {"success": False, "error": "PyMuPDF not available. Install pymupdf."},
                ensure_ascii=False,
            )
        )
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}, ensure_ascii=False))
        sys.exit(1)


if __name__ == "__main__":
    main()
