#!/usr/bin/env python3
"""Build a privacy-isolated, model-agnostic image-input candidate.

This tool never performs OCR or field parsing. It verifies the frozen source
manifest, creates derived images in an ignored owner-only directory, and writes
only image-processing metadata to its candidate manifest.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
import shutil
import stat
import tempfile
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
from PIL import Image, ImageFilter, ImageOps


PROJECT_DIRECTORY = Path(__file__).resolve().parent.parent
PRIVATE_EVALUATION_ROOT = (
    PROJECT_DIRECTORY / "tests" / "evaluation" / "real-local"
).resolve()
APPROVED_MANIFEST_SHA256 = (
    "cd96056be80b4670c7a315ddcdb37dc5f6a015367013be9bd7336a967157c610"
)
MANIFEST_VERSION = "m1-image-input-candidate/2"
PROFILE_VERSION = "document-normalize/3-candidate-c"
LOW_RES_LONG_EDGE = 1_600
LOW_RES_SCALE = 2
UNSHARP_RADIUS = 1.2
UNSHARP_PERCENT = 80
UNSHARP_THRESHOLD = 3
GRID_OVERVIEW_LONG_EDGE = 1_024
GRID_COLUMNS = 2
GRID_ROWS = 2
GRID_OVERLAP_RATIO = 0.12
GRID_JPEG_QUALITY = 95
MAX_OUTPUT_BYTES = 20 * 1024 * 1024


def sha256_bytes(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def require_owner_only_file(path: Path, label: str) -> bytes:
    information = path.stat()
    if not stat.S_ISREG(information.st_mode) or information.st_mode & 0o077:
        raise ValueError(f"{label} must be a regular owner-only file")
    return path.read_bytes()


def require_private_output_path(path: Path) -> None:
    resolved = path.resolve()
    if resolved == PRIVATE_EVALUATION_ROOT or PRIVATE_EVALUATION_ROOT not in resolved.parents:
        raise ValueError("output directory must be inside tests/evaluation/real-local")
    if resolved.exists():
        raise ValueError("output directory already exists")


def luminance_metrics(image: Image.Image) -> dict[str, float]:
    luminance = np.asarray(image.convert("L"), dtype=np.uint8)
    total = luminance.size
    return {
        "black_clip_percent": round(float(np.count_nonzero(luminance <= 2)) * 100 / total, 4),
        "white_clip_percent": round(float(np.count_nonzero(luminance >= 253)) * 100 / total, 4),
        "mean_luminance": round(float(luminance.mean()), 4),
        "luminance_stddev": round(float(luminance.std()), 4),
    }


def use_high_resolution_grid(width: int, height: int) -> bool:
    return max(width, height) >= LOW_RES_LONG_EDGE


def enhance_low_resolution_image(
    source: Image.Image,
) -> tuple[Image.Image, dict[str, object]]:
    oriented = ImageOps.exif_transpose(source).convert("RGB")
    source_width, source_height = oriented.size
    if use_high_resolution_grid(source_width, source_height):
        raise ValueError("high-resolution image must use the grid profile")
    oriented = oriented.resize(
        (source_width * LOW_RES_SCALE, source_height * LOW_RES_SCALE),
        Image.Resampling.LANCZOS,
    )

    luminance, chroma_blue, chroma_red = oriented.convert("YCbCr").split()
    luminance = luminance.filter(
        ImageFilter.UnsharpMask(
            radius=UNSHARP_RADIUS,
            percent=UNSHARP_PERCENT,
            threshold=UNSHARP_THRESHOLD,
        )
    )
    enhanced = Image.merge(
        "YCbCr",
        (luminance, chroma_blue, chroma_red),
    ).convert("RGB")
    return enhanced, {
        "source_width": source_width,
        "source_height": source_height,
        "output_width": enhanced.width,
        "output_height": enhanced.height,
        "scale_factor": LOW_RES_SCALE,
    }


def encode_png(image: Image.Image) -> tuple[bytes, str, str]:
    output = io.BytesIO()
    image.save(output, format="PNG", compress_level=6)
    return output.getvalue(), "image/png", ".png"


def encode_jpeg(image: Image.Image) -> tuple[bytes, str, str]:
    output = io.BytesIO()
    image.save(
        output,
        format="JPEG",
        quality=GRID_JPEG_QUALITY,
        subsampling=0,
        optimize=True,
    )
    return output.getvalue(), "image/jpeg", ".jpg"


def grid_axis(length: int, parts: int, overlap_ratio: float) -> list[tuple[int, int]]:
    if parts < 1 or length < parts:
        raise ValueError("invalid grid axis")
    if parts == 1:
        return [(0, length)]
    tile_length = int(np.ceil(length / (parts - (parts - 1) * overlap_ratio)))
    positions = []
    for index in range(parts):
        start = round(index * (length - tile_length) / (parts - 1))
        positions.append((start, start + tile_length))
    if positions[0][0] != 0 or positions[-1][1] != length:
        raise ValueError("grid does not cover the full axis")
    return positions


def high_resolution_views(
    source: Image.Image,
) -> list[tuple[str, Image.Image, dict[str, object]]]:
    oriented = ImageOps.exif_transpose(source).convert("RGB")
    width, height = oriented.size
    if not use_high_resolution_grid(width, height):
        raise ValueError("low-resolution image must not use the grid profile")

    overview_scale = min(1.0, GRID_OVERVIEW_LONG_EDGE / max(width, height))
    overview = oriented.resize(
        (
            max(1, round(width * overview_scale)),
            max(1, round(height * overview_scale)),
        ),
        Image.Resampling.LANCZOS,
    )
    result = [
        (
            "overview",
            overview,
            {
                "crop_box": [0, 0, width, height],
                "source_scale": round(overview_scale, 6),
            },
        )
    ]
    horizontal = grid_axis(width, GRID_COLUMNS, GRID_OVERLAP_RATIO)
    vertical = grid_axis(height, GRID_ROWS, GRID_OVERLAP_RATIO)
    for row, (top, bottom) in enumerate(vertical):
        for column, (left, right) in enumerate(horizontal):
            result.append(
                (
                    f"tile-{row}-{column}",
                    oriented.crop((left, top, right, bottom)),
                    {
                        "crop_box": [left, top, right, bottom],
                        "source_scale": 1,
                    },
                )
            )
    return result


def validate_guardrails(
    source_metrics: dict[str, float],
    candidate_metrics: dict[str, float],
    encoded: bytes,
) -> None:
    if not encoded or len(encoded) > MAX_OUTPUT_BYTES:
        raise ValueError("candidate image exceeds the protected byte boundary")
    if candidate_metrics["black_clip_percent"] > source_metrics["black_clip_percent"] + 5:
        raise ValueError("candidate image failed black_clip_percent guardrail")
    if candidate_metrics["white_clip_percent"] > source_metrics["white_clip_percent"] + 10:
        raise ValueError("candidate image failed white_clip_percent guardrail")
    if candidate_metrics["luminance_stddev"] > max(
        1.0,
        source_metrics["luminance_stddev"] * 3,
    ):
        raise ValueError("candidate image contrast increased beyond the guardrail")


def build_candidate(
    manifest_path: Path,
    sample_ids: list[str],
    output_directory: Path,
) -> dict[str, object]:
    require_private_output_path(output_directory)
    manifest_bytes = require_owner_only_file(manifest_path, "manifest")
    if sha256_bytes(manifest_bytes) != APPROVED_MANIFEST_SHA256:
        raise ValueError("manifest is not the approved frozen m1-real-dev-v4")
    manifest = json.loads(manifest_bytes)
    if len(sample_ids) != 5 or len(set(sample_ids)) != 5:
        raise ValueError("candidate build requires exactly five distinct samples")

    samples_by_id = {sample["sample_id"]: sample for sample in manifest["samples"]}
    selected = []
    for sample_id in sample_ids:
        sample = samples_by_id.get(sample_id)
        if not sample or sample.get("document_type") != "invoice":
            raise ValueError(f"{sample_id} is not an invoice sample")
        selected.append(sample)

    output_parent = output_directory.parent
    output_parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(output_parent, 0o700)
    temporary = Path(
        tempfile.mkdtemp(prefix=".document-normalize-3-", dir=output_parent)
    )
    os.chmod(temporary, 0o700)
    try:
        assets_directory = temporary / "assets"
        assets_directory.mkdir(mode=0o700)
        candidate_samples = []
        for sample in selected:
            source_path = (manifest_path.parent / sample["file"]).resolve()
            source_bytes = require_owner_only_file(source_path, sample["sample_id"])
            if sha256_bytes(source_bytes) != sample["sha256"]:
                raise ValueError(f"{sample['sample_id']}: source image hash mismatch")
            with Image.open(io.BytesIO(source_bytes)) as decoded:
                source_rgb = ImageOps.exif_transpose(decoded).convert("RGB")
            source_metrics = luminance_metrics(source_rgb)
            source_width, source_height = source_rgb.size
            use_grid = use_high_resolution_grid(source_width, source_height)
            if use_grid:
                rendered_views = [
                    (kind, image, metadata, encode_jpeg)
                    for kind, image, metadata in high_resolution_views(source_rgb)
                ]
                strategy = "high_resolution_grid"
            else:
                enhanced, dimensions = enhance_low_resolution_image(source_rgb)
                rendered_views = [
                    (
                        "enhanced-overview",
                        enhanced,
                        {
                            "crop_box": [0, 0, source_width, source_height],
                            "source_scale": dimensions["scale_factor"],
                        },
                        encode_png,
                    )
                ]
                strategy = "low_resolution_enhanced"

            views = []
            for kind, image, view_metadata, encoder in rendered_views:
                encoded, declared_mime, extension = encoder(image)
                candidate_metrics = luminance_metrics(image)
                if strategy == "low_resolution_enhanced":
                    validate_guardrails(source_metrics, candidate_metrics, encoded)
                elif not encoded or len(encoded) > MAX_OUTPUT_BYTES:
                    raise ValueError(
                        f"{sample['sample_id']}/{kind}: view exceeds the protected byte boundary"
                    )

                filename = f"{sample['sample_id']}-{kind}{extension}"
                if Path(filename).name != filename:
                    raise ValueError("candidate filename is unsafe")
                candidate_path = assets_directory / filename
                with candidate_path.open("xb") as output:
                    output.write(encoded)
                os.chmod(candidate_path, 0o600)
                views.append(
                    {
                        "kind": kind,
                        "file": f"assets/{filename}",
                        "declared_mime": declared_mime,
                        "sha256": sha256_bytes(encoded),
                        "output_bytes": len(encoded),
                        "output_width": image.width,
                        "output_height": image.height,
                        **view_metadata,
                        "candidate_luminance_metrics": candidate_metrics,
                    }
                )

            candidate_samples.append(
                {
                    "sample_id": sample["sample_id"],
                    "source_sha256": sample["sha256"],
                    "strategy": strategy,
                    "source_width": source_width,
                    "source_height": source_height,
                    "source_luminance_metrics": source_metrics,
                    "views": views,
                }
            )

        candidate_manifest = {
            "manifest_version": MANIFEST_VERSION,
            "profile_version": PROFILE_VERSION,
            "source_manifest_sha256": APPROVED_MANIFEST_SHA256,
            "created_at": datetime.now(timezone.utc).isoformat(),
            "sample_count": len(candidate_samples),
            "parameters": {
                "exif_transpose": True,
                "color_mode": "RGB",
                "low_resolution_long_edge_threshold": LOW_RES_LONG_EDGE,
                "low_resolution_scale": LOW_RES_SCALE,
                "resize_filter": "Lanczos",
                "unsharp_mask": {
                    "radius": UNSHARP_RADIUS,
                    "percent": UNSHARP_PERCENT,
                    "threshold": UNSHARP_THRESHOLD,
                },
                "high_resolution_behavior": "same_page_overview_plus_overlapping_grid",
                "grid": {
                    "overview_long_edge": GRID_OVERVIEW_LONG_EDGE,
                    "columns": GRID_COLUMNS,
                    "rows": GRID_ROWS,
                    "overlap_ratio": GRID_OVERLAP_RATIO,
                    "format": "JPEG",
                    "quality": GRID_JPEG_QUALITY,
                    "subsampling": "4:4:4",
                },
            },
            "privacy": {
                "contains_field_labels": False,
                "contains_model_output": False,
                "owner_only": True,
                "git_ignored": True,
            },
            "samples": candidate_samples,
        }
        encoded_manifest = (
            json.dumps(candidate_manifest, ensure_ascii=False, indent=2) + "\n"
        ).encode("utf-8")
        manifest_output = temporary / "manifest.json"
        with manifest_output.open("xb") as output:
            output.write(encoded_manifest)
        os.chmod(manifest_output, 0o600)
        temporary.rename(output_directory)
        return {
            "profile_version": PROFILE_VERSION,
            "sample_count": len(candidate_samples),
            "candidate_manifest_sha256": sha256_bytes(encoded_manifest),
            "guardrails_passed": True,
        }
    except Exception:
        shutil.rmtree(temporary, ignore_errors=True)
        raise


def run_self_test() -> None:
    source = Image.new("RGB", (320, 180))
    for y in range(source.height):
        level = 205 + (y * 35 // source.height)
        for x in range(source.width):
            source.putpixel((x, y), (level, level, level))
    for x in range(40, 280, 12):
        for y in range(35, 145):
            source.putpixel((x, y), (48, 52, 58))
    enhanced, dimensions = enhance_low_resolution_image(source)
    encoded, declared_mime, extension = encode_png(enhanced)
    validate_guardrails(
        luminance_metrics(source),
        luminance_metrics(enhanced),
        encoded,
    )
    assert dimensions["scale_factor"] == 2
    assert enhanced.size == (640, 360)
    assert declared_mime == "image/png"
    assert extension == ".png"
    assert use_high_resolution_grid(1_600, 900) is True
    assert use_high_resolution_grid(1_599, 900) is False

    high_resolution = Image.new("RGB", (1_600, 900), "white")
    views = high_resolution_views(high_resolution)
    assert [kind for kind, _, _ in views] == [
        "overview",
        "tile-0-0",
        "tile-0-1",
        "tile-1-0",
        "tile-1-1",
    ]
    assert views[0][1].size == (1_024, 576)
    for _, view, metadata in views:
        encoded_view, mime, extension = encode_jpeg(view)
        assert encoded_view
        assert mime == "image/jpeg"
        assert extension == ".jpg"
        left, top, right, bottom = metadata["crop_box"]
        assert 0 <= left < right <= high_resolution.width
        assert 0 <= top < bottom <= high_resolution.height
    assert grid_axis(1_600, 2, GRID_OVERLAP_RATIO)[0][1] > grid_axis(
        1_600, 2, GRID_OVERLAP_RATIO
    )[1][0]


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--sample-ids")
    parser.add_argument("--output-directory", type=Path)
    parser.add_argument("--self-test", action="store_true")
    arguments = parser.parse_args()
    if arguments.self_test:
        return arguments
    for name in ("manifest", "sample_ids", "output_directory"):
        if getattr(arguments, name) in (None, ""):
            parser.error(f"--{name.replace('_', '-')} is required")
    return arguments


def main() -> None:
    arguments = parse_arguments()
    if arguments.self_test:
        run_self_test()
        print("image input candidate self-test passed")
        return
    sample_ids = [
        sample_id.strip()
        for sample_id in arguments.sample_ids.split(",")
        if sample_id.strip()
    ]
    result = build_candidate(
        arguments.manifest.resolve(),
        sample_ids,
        arguments.output_directory.resolve(),
    )
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
