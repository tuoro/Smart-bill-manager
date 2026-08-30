#!/usr/bin/env python3
"""生成 M1 Prompt 调优集 v2；不读取或复制发布评测资产。"""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
from typing import Any

from PIL import Image, ImageDraw, ImageFont

import generate as v1


DATASET_VERSION = "m1-prompt-dev-v2"
V1_MANIFEST_SHA256 = "1bea5129832ea2e536792f7b023c1df61f4f9113ed180de66b67b7a7724d89f3"
V1_GENERATOR_SHA256 = "60f4af2c61024068ed4799537ccc10f48ae6ae1ca55805a6d2dcab7d1375f3fb"
IMAGE_SIZE = (600, 400)


def tagged(sample: dict[str, Any], *tags: str) -> dict[str, Any]:
    sample["scenario_tags"] = [*sample["scenario_tags"], "compact_bitmap", *tags]
    return sample


def build_samples() -> list[dict[str, Any]]:
    payments = [
        tagged(
            v1.payment_sample(
                "TUNE2-PAY-001",
                merchant="Literal Dot Market",
                currency="USD",
                transaction_time="2026-09-11T07:08:09-05:00",
                timezone="America/Chicago",
                amount_minor=907,
                total_lines=["TOTAL: USD 9.07"],
                amount_quote="USD 9.07",
                optional={
                    "payment_method": "Test Card 2048",
                    "order_number": "LIT-PAY-09-001",
                },
            ),
            "literal_evidence",
            "dot_decimal",
        ),
        tagged(
            v1.payment_sample(
                "TUNE2-PAY-002",
                merchant="Comma Proof Studio",
                currency="EUR",
                transaction_time="2026-09-12T18:03:04+02:00",
                timezone="Europe/Paris",
                amount_minor=876543,
                total_lines=["TOTAL: EUR 8.765,43"],
                amount_quote="EUR 8.765,43",
                optional={"category": "Design-Lab"},
            ),
            "literal_evidence",
            "comma_decimal",
        ),
        tagged(
            v1.payment_sample(
                "TUNE2-PAY-003",
                merchant="Low-Contrast Corner",
                currency="CNY",
                transaction_time="2026-09-13T06:05:04+08:00",
                timezone="Asia/Shanghai",
                amount_minor=1234,
                total_lines=["TOTAL: CNY 12.34"],
                amount_quote="CNY 12.34",
                optional={"order_number": "LC-03-A7"},
            ),
            "literal_evidence",
            "low_contrast",
        ),
        tagged(
            v1.payment_sample(
                "TUNE2-PAY-004",
                merchant="Zero Yen Kiosk",
                currency="JPY",
                transaction_time="2026-09-14T00:01:02+09:00",
                timezone="Asia/Tokyo",
                amount_minor=4070,
                total_lines=["TOTAL: JPY 4070"],
                amount_quote="JPY 4070",
                optional={
                    "payment_method": "Synthetic Cash",
                    "category": "Transit",
                },
            ),
            "literal_evidence",
            "zero_decimal_currency",
        ),
        tagged(
            v1.payment_sample(
                "TUNE2-PAY-005",
                merchant="Absent Total Workshop",
                currency="CNY",
                transaction_time="2026-09-15T12:13:14+08:00",
                timezone="Asia/Shanghai",
                amount_minor=None,
                total_lines=["TOTAL: [NOT VISIBLE]"],
                event="missing:amount_minor",
            ),
            "literal_evidence",
            "low_contrast",
        ),
        tagged(
            v1.payment_sample(
                "TUNE2-PAY-006",
                merchant="Dual Total Workshop",
                currency="USD",
                transaction_time="2026-09-16T21:22:23+00:00",
                timezone="UTC",
                amount_minor=None,
                total_lines=["TOTAL_A: USD 31.09", "TOTAL_B: USD 31.90"],
                event="conflict:amount_minor",
            ),
            "literal_evidence",
            "conflicting_punctuation",
        ),
    ]

    invoices = [
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-001",
                invoice_number="LIT-INV-2026-0917-A",
                invoice_date="2026-09-17",
                currency="USD",
                seller="Exact Name Supply",
                buyer="Colon Test Labs",
                total_minor=4242,
                total_lines=["TOTAL: USD 42.42"],
                total_quote="USD 42.42",
                tax_minor=342,
                tax_quote="USD 3.42",
                items=[{
                    "name": "Line-One Service",
                    "quantity": "1",
                    "unit": "job",
                    "unit_price_minor": 3900,
                    "amount_minor": 3900,
                    "tax_minor": 342,
                    "sort_order": 0,
                    "display": {
                        "unit_price_minor": "USD 39.00",
                        "amount_minor": "USD 39.00",
                        "tax_minor": "USD 3.42",
                    },
                }],
            ),
            "literal_evidence",
            "root_key_guard",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-002",
                invoice_number="LIT-INV-2026-0918-B",
                invoice_date="2026-09-18",
                currency="CNY",
                seller="Dash-Name Seller",
                buyer="Buyer 18-B",
                total_minor=10880,
                total_lines=["TOTAL: CNY 108.80"],
                total_quote="CNY 108.80",
                tax_minor=None,
                tax_quote=None,
                items=[
                    {
                        "name": "Alpha-Part",
                        "quantity": "2",
                        "unit": "pc",
                        "unit_price_minor": 2440,
                        "amount_minor": 4880,
                        "sort_order": 0,
                        "display": {
                            "unit_price_minor": "CNY 24.40",
                            "amount_minor": "CNY 48.80",
                        },
                    },
                    {
                        "name": "Beta-Part",
                        "quantity": "1",
                        "unit": "pc",
                        "unit_price_minor": 6000,
                        "amount_minor": 6000,
                        "sort_order": 1,
                        "display": {
                            "unit_price_minor": "CNY 60.00",
                            "amount_minor": "CNY 60.00",
                        },
                    },
                ],
            ),
            "literal_evidence",
            "multi_item_exact_order",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-003",
                invoice_number="EU-LITERAL-19-C",
                invoice_date="2026-09-19",
                currency="EUR",
                seller="Punkt und Komma GmbH",
                buyer="Literal Buyer SARL",
                total_minor=120050,
                total_lines=["TOTAL: EUR 1.200,50"],
                total_quote="EUR 1.200,50",
                tax_minor=20050,
                tax_quote="EUR 200,50",
                items=[{
                    "name": "Format-Pruefung",
                    "quantity": "2.5",
                    "unit": "hour",
                    "unit_price_minor": 40000,
                    "amount_minor": 100000,
                    "tax_minor": 20050,
                    "sort_order": 0,
                    "display": {
                        "unit_price_minor": "EUR 400,00",
                        "amount_minor": "EUR 1.000,00",
                        "tax_minor": "EUR 200,50",
                    },
                }],
            ),
            "literal_evidence",
            "comma_decimal",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-004",
                invoice_number="GRAY-INV-20-D",
                invoice_date="2026-09-20",
                currency="JPY",
                seller="Gray Pixel Works",
                buyer="Tiny Type Office",
                total_minor=7650,
                total_lines=["TOTAL: JPY 7650"],
                total_quote="JPY 7650",
                tax_minor=None,
                tax_quote=None,
                items=[{
                    "name": "Compact Print",
                    "amount_minor": 7650,
                    "sort_order": 0,
                    "display": {"amount_minor": "JPY 7650"},
                }],
            ),
            "literal_evidence",
            "low_contrast",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-005",
                invoice_number=None,
                invoice_date="2026-09-21",
                currency="USD",
                seller="No Number Seller",
                buyer="Review Queue Buyer",
                total_minor=1500,
                total_lines=["TOTAL: USD 15.00"],
                total_quote="USD 15.00",
                tax_minor=None,
                tax_quote=None,
                items=[{
                    "name": "Review Item",
                    "amount_minor": 1500,
                    "sort_order": 0,
                    "display": {"amount_minor": "USD 15.00"},
                }],
                event="missing:invoice_number",
            ),
            "literal_evidence",
            "low_contrast",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-006",
                invoice_number="DUAL-INV-22-F",
                invoice_date="2026-09-22",
                currency="CNY",
                seller="Conflict Seller",
                buyer="Conflict Buyer",
                total_minor=None,
                total_lines=["TOTAL_A: CNY 71.10", "TOTAL_B: CNY 17.10"],
                total_quote=None,
                tax_minor=None,
                tax_quote=None,
                items=[{
                    "name": "Visible Item",
                    "amount_minor": 1710,
                    "sort_order": 0,
                    "display": {"amount_minor": "CNY 17.10"},
                }],
                event="conflict:total_minor",
            ),
            "literal_evidence",
            "conflicting_punctuation",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-007",
                invoice_number="TYPE-INV-23-G",
                invoice_date="2026-09-23",
                currency="USD",
                seller="Typed Value Seller",
                buyer="Typed Value Buyer",
                total_minor=5050,
                total_lines=["TOTAL: USD 50.50"],
                total_quote="USD 50.50",
                tax_minor=50,
                tax_quote="USD 0.50",
                items=[{
                    "name": "Quarter Hour",
                    "quantity": "1.25",
                    "unit": "hour",
                    "unit_price_minor": 4000,
                    "amount_minor": 5000,
                    "tax_minor": 50,
                    "sort_order": 0,
                    "display": {
                        "unit_price_minor": "USD 40.00",
                        "amount_minor": "USD 50.00",
                        "tax_minor": "USD 0.50",
                    },
                }],
            ),
            "literal_evidence",
            "typed_value_contract",
        ),
        tagged(
            v1.invoice_sample(
                "TUNE2-INV-008",
                invoice_number="ORDER-INV-24-H",
                invoice_date="2026-09-24",
                currency="EUR",
                seller="Ordered Rows Seller",
                buyer="Ordered Rows Buyer",
                total_minor=6666,
                total_lines=["TOTAL: EUR 66,66"],
                total_quote="EUR 66,66",
                tax_minor=None,
                tax_quote=None,
                items=[
                    {"name": "Row-A", "amount_minor": 1111, "sort_order": 0, "display": {"amount_minor": "EUR 11,11"}},
                    {"name": "Row-B", "amount_minor": 2222, "sort_order": 1, "display": {"amount_minor": "EUR 22,22"}},
                    {"name": "Row-C", "amount_minor": 3333, "sort_order": 2, "display": {"amount_minor": "EUR 33,33"}},
                ],
            ),
            "literal_evidence",
            "multi_item_exact_order",
            "root_key_guard",
        ),
    ]

    unknowns = [
        tagged(
            v1.unknown_sample(
                "TUNE2-UNK-001",
                ["SYNTHETIC DELIVERY LABEL", "REFERENCE: DEV2-SHIP-25", "NOT A PAYMENT OR INVOICE"],
            ),
            "root_key_guard",
            "low_contrast",
        ),
        tagged(
            v1.unknown_sample(
                "TUNE2-UNK-002",
                ["SYNTHETIC MEETING NOTE", "REFERENCE: DEV2-NOTE-26", "NO FINANCIAL CLAIMS"],
            ),
            "root_key_guard",
        ),
    ]
    return [*payments, *invoices, *unknowns]


def render_image(path: Path, lines: list[str], *, low_contrast: bool) -> None:
    background = (247, 248, 250) if low_contrast else (250, 251, 253)
    paper = (252, 252, 251) if low_contrast else (255, 255, 255)
    ink = (116, 120, 126) if low_contrast else (25, 36, 52)
    image = Image.new("RGB", IMAGE_SIZE, background)
    draw = ImageDraw.Draw(image)
    draw.rectangle((12, 12, 587, 387), fill=paper, outline=(211, 216, 223), width=1)
    font = ImageFont.truetype(str(v1.FONT_PATH), 10)
    header_font = ImageFont.truetype(str(v1.FONT_PATH), 11)
    y = 27
    for index, line in enumerate(lines):
        selected_font = header_font if index < 2 else font
        if draw.textlength(line, font=selected_font) > 548:
            raise ValueError(f"line exceeds compact bitmap width: {line}")
        draw.text((25, y), line, font=selected_font, fill=ink)
        y += 21
    if y > 382:
        raise ValueError(f"{path.name}: content exceeds compact bitmap height")
    if low_contrast:
        scanline = (238, 239, 241)
        for x in range(19, IMAGE_SIZE[0], 41):
            draw.line((x, 13, x, 386), fill=scanline, width=1)
    image.save(path, format="PNG", optimize=False, compress_level=9)
    os.chmod(path, 0o600)


def sha256_bytes(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def ensure_frozen_dependencies(tuning_root: Path) -> None:
    v1.ensure_runtime()
    if sha256_bytes(Path(v1.__file__).read_bytes()) != V1_GENERATOR_SHA256:
        raise RuntimeError("frozen v1 generator dependency changed")
    if sha256_bytes((tuning_root / "manifest-v1.json").read_bytes()) != V1_MANIFEST_SHA256:
        raise RuntimeError("frozen v1 tuning manifest changed")


def main() -> None:
    tuning_root = Path(__file__).resolve().parent.parent
    ensure_frozen_dependencies(tuning_root)
    asset_root = tuning_root / "assets" / DATASET_VERSION
    asset_root.mkdir(parents=True, exist_ok=True, mode=0o700)
    samples = build_samples()
    if len(samples) != 16:
        raise RuntimeError(f"sample count = {len(samples)}, want 16")

    expected_names: set[str] = set()
    for sample in samples:
        name = sample["original_name"]
        expected_names.add(name)
        path = asset_root / name
        render_image(
            path,
            sample.pop("render_lines"),
            low_contrast="low_contrast" in sample["scenario_tags"],
        )
        sample["file"] = f"assets/{DATASET_VERSION}/{name}"
        sample["sha256"] = sha256_bytes(path.read_bytes())
    for existing in asset_root.iterdir():
        if existing.is_file() and existing.name not in expected_names:
            raise RuntimeError(f"unlisted tuning asset exists: {existing.name}")

    manifest = {
        "dataset_version": DATASET_VERSION,
        "created_at": "2026-08-29T00:00:00Z",
        "synthetic_only": True,
        "intended_use": "prompt_provider_contract_tuning_only",
        "excluded_from_release_evidence": True,
        "source_dataset_versions": [],
        "supersedes_dataset_version": "m1-prompt-dev-v1",
        "generator": "tests/evaluation/tuning/generator/generate_v2.py",
        "generator_dependencies": {
            "v1_generator_sha256": V1_GENERATOR_SHA256,
            "v1_manifest_sha256": V1_MANIFEST_SHA256,
        },
        "generator_runtime": {
            "python": v1.PYTHON_VERSION,
            "pillow": v1.PILLOW_VERSION,
            "font": str(v1.FONT_PATH),
            "font_sha256": v1.FONT_SHA256,
        },
        "render_profile": {
            "width": IMAGE_SIZE[0],
            "height": IMAGE_SIZE[1],
            "body_font_px": 10,
            "header_font_px": 11,
            "low_contrast_assets": 5,
        },
        "samples": samples,
    }
    encoded = (json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode()
    manifest_path = tuning_root / "manifest-v2.json"
    manifest_path.write_bytes(encoded)
    os.chmod(manifest_path, 0o600)
    print(f"generated {len(samples)} independent tuning samples in {asset_root}")


if __name__ == "__main__":
    main()
