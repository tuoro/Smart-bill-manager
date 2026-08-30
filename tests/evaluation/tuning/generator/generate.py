#!/usr/bin/env python3
"""生成独立于发布评测集的 M1 Prompt 调优图片与清单。"""

from __future__ import annotations

import hashlib
import json
import os
import sys
from pathlib import Path
from typing import Any

import PIL
from PIL import Image, ImageDraw, ImageFont


DATASET_VERSION = "m1-prompt-dev-v1"
PYTHON_VERSION = "3.12.13"
PILLOW_VERSION = "12.3.0"
FONT_PATH = Path("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf")
FONT_SHA256 = "a54dca07c76d6289e717e75e0a58c0128f6d7269ef3faf76417c9d7d3bba37ab"

PAYMENT_TYPES = {
    "amount_minor": "money_minor",
    "currency": "string",
    "merchant": "string",
    "transaction_time": "instant",
    "source_timezone": "string",
    "payment_method": "string",
    "order_number": "string",
    "category": "string",
}
INVOICE_TYPES = {
    "invoice_number": "string",
    "invoice_date": "date",
    "total_minor": "money_minor",
    "tax_minor": "money_minor",
    "currency": "string",
    "seller_name": "string",
    "buyer_name": "string",
}
ITEM_TYPES = {
    "name": "string",
    "quantity": "decimal",
    "unit": "string",
    "unit_price_minor": "money_minor",
    "amount_minor": "money_minor",
    "tax_minor": "money_minor",
    "sort_order": "integer",
}


def sha256_bytes(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def ensure_runtime() -> None:
    if sys.version.split()[0] != PYTHON_VERSION:
        raise RuntimeError(f"Python {PYTHON_VERSION} required, got {sys.version.split()[0]}")
    if PIL.__version__ != PILLOW_VERSION:
        raise RuntimeError(f"Pillow {PILLOW_VERSION} required, got {PIL.__version__}")
    if sha256_bytes(FONT_PATH.read_bytes()) != FONT_SHA256:
        raise RuntimeError("pinned synthetic font hash does not match")


def payment_sample(
    sample_id: str,
    *,
    merchant: str,
    currency: str,
    transaction_time: str,
    timezone: str,
    amount_minor: int | None,
    total_lines: list[str],
    amount_quote: str | None = None,
    optional: dict[str, str] | None = None,
    event: str | None = None,
) -> dict[str, Any]:
    optional = optional or {}
    fields: dict[str, Any] = {
        "currency": currency,
        "merchant": merchant,
        "transaction_time": transaction_time,
        "source_timezone": timezone,
        **optional,
    }
    evidence: dict[str, dict[str, Any]] = {
        "currency": {"page": 1, "quote": currency},
        "merchant": {"page": 1, "quote": merchant},
        "transaction_time": {"page": 1, "quote": transaction_time},
        "source_timezone": {"page": 1, "quote": timezone},
    }
    if amount_minor is not None:
        if amount_quote is None:
            raise ValueError(f"{sample_id}: amount quote is required")
        fields["amount_minor"] = amount_minor
        evidence["amount_minor"] = {"page": 1, "quote": amount_quote}
    optional_labels = {
        "payment_method": "PAYMENT_METHOD",
        "order_number": "ORDER_NUMBER",
        "category": "CATEGORY",
    }
    lines = [
        "SYNTHETIC ONLY - NO REAL TRANSACTION",
        "SYNTHETIC PAYMENT RECEIPT",
        f"MERCHANT: {merchant}",
        *total_lines,
        f"CURRENCY: {currency}",
        f"TRANSACTION_TIME: {transaction_time}",
        f"SOURCE_TIMEZONE: {timezone}",
    ]
    for path in ("payment_method", "order_number", "category"):
        if path in optional:
            lines.append(f"{optional_labels[path]}: {optional[path]}")
            evidence[path] = {"page": 1, "quote": optional[path]}
    events = [] if event is None else [event]
    return finalize_sample(
        sample_id,
        "payment",
        ["payment_screenshot", *( ["missing_conflict"] if event else [])],
        lines,
        fields,
        PAYMENT_TYPES,
        evidence,
        events,
        "blocked" if event else "needs_review",
    )


def invoice_sample(
    sample_id: str,
    *,
    invoice_number: str | None,
    invoice_date: str,
    currency: str,
    seller: str,
    buyer: str,
    total_minor: int | None,
    total_lines: list[str],
    total_quote: str | None,
    tax_minor: int | None,
    tax_quote: str | None,
    items: list[dict[str, Any]],
    event: str | None = None,
) -> dict[str, Any]:
    fields: dict[str, Any] = {
        "invoice_date": invoice_date,
        "currency": currency,
        "seller_name": seller,
        "buyer_name": buyer,
    }
    evidence: dict[str, dict[str, Any]] = {
        "invoice_date": {"page": 1, "quote": invoice_date},
        "currency": {"page": 1, "quote": currency},
        "seller_name": {"page": 1, "quote": seller},
        "buyer_name": {"page": 1, "quote": buyer},
    }
    if invoice_number is not None:
        fields["invoice_number"] = invoice_number
        evidence["invoice_number"] = {"page": 1, "quote": invoice_number}
    if total_minor is not None:
        if total_quote is None:
            raise ValueError(f"{sample_id}: total quote is required")
        fields["total_minor"] = total_minor
        evidence["total_minor"] = {"page": 1, "quote": total_quote}
    if tax_minor is not None:
        if tax_quote is None:
            raise ValueError(f"{sample_id}: tax quote is required")
        fields["tax_minor"] = tax_minor
        evidence["tax_minor"] = {"page": 1, "quote": tax_quote}

    number_line = (
        f"INVOICE_NUMBER: {invoice_number}"
        if invoice_number is not None
        else "INVOICE_NUMBER: [MISSING]"
    )
    lines = [
        "SYNTHETIC ONLY - NO REAL TRANSACTION",
        "SYNTHETIC TAX INVOICE",
        number_line,
        f"INVOICE_DATE: {invoice_date}",
        f"SELLER: {seller}",
        f"BUYER: {buyer}",
        *total_lines,
        f"CURRENCY: {currency}",
    ]
    if tax_minor is not None:
        lines.append(f"TAX: {tax_quote}")

    value_types = dict(INVOICE_TYPES)
    for index, item in enumerate(items):
        prefix = f"items[{index}]."
        for name, value_type in ITEM_TYPES.items():
            value_types[prefix + name] = value_type
        first_line = (
            f"ITEM {index} | NAME: {item['name']} | SORT_ORDER: {item['sort_order']}"
        )
        detail_parts: list[str] = []
        labels = {
            "quantity": "QTY",
            "unit": "UNIT",
            "unit_price_minor": "UNIT_PRICE",
            "amount_minor": "AMOUNT",
            "tax_minor": "TAX",
        }
        display = item["display"]
        for name in ("quantity", "unit", "unit_price_minor", "amount_minor", "tax_minor"):
            if name in item:
                detail_parts.append(f"{labels[name]}: {display.get(name, item[name])}")
        second_line = " | ".join(detail_parts)
        lines.extend([first_line, second_line])
        for name in ITEM_TYPES:
            path = prefix + name
            if name in item:
                fields[path] = item[name]
                if name == "name":
                    quote = f"NAME: {item[name]}"
                elif name == "sort_order":
                    quote = f"SORT_ORDER: {item[name]}"
                else:
                    quote = f"{labels[name]}: {display.get(name, item[name])}"
                evidence[path] = {
                    "page": 1,
                    "quote": quote,
                }
    events = [] if event is None else [event]
    tags = ["single_item_invoice" if len(items) == 1 else "multi_item_invoice"]
    if event:
        tags.append("missing_conflict")
    return finalize_sample(
        sample_id,
        "invoice",
        tags,
        lines,
        fields,
        value_types,
        evidence,
        events,
        "blocked" if event else "needs_review",
    )


def unknown_sample(sample_id: str, lines: list[str]) -> dict[str, Any]:
    return finalize_sample(
        sample_id,
        "unknown",
        ["supported_unknown"],
        ["SYNTHETIC ONLY - NO REAL TRANSACTION", *lines],
        {},
        {},
        {},
        ["unknown_document_type"],
        "blocked",
    )


def finalize_sample(
    sample_id: str,
    document_type: str,
    tags: list[str],
    lines: list[str],
    fields: dict[str, Any],
    value_types: dict[str, str],
    evidence: dict[str, dict[str, Any]],
    events: list[str],
    review_state: str,
) -> dict[str, Any]:
    missing = [path for path in value_types if path not in fields]
    for path, value in fields.items():
        if path not in value_types:
            raise ValueError(f"{sample_id}: no value type for {path}")
        if path not in evidence:
            raise ValueError(f"{sample_id}: no evidence for {path}")
        quote = evidence[path]["quote"]
        if not any(quote in line for line in lines):
            raise ValueError(f"{sample_id}: evidence quote is not visible for {path}: {quote}")
        expected_type = value_types[path]
        if expected_type in {"money_minor", "integer"} and (
            not isinstance(value, int) or isinstance(value, bool)
        ):
            raise ValueError(f"{sample_id}: {path} must be an integer")
        if expected_type in {"string", "date", "instant", "decimal"} and not isinstance(value, str):
            raise ValueError(f"{sample_id}: {path} must be a string")
    return {
        "sample_id": sample_id,
        "file": "",
        "sha256": "",
        "original_name": sample_id.lower() + ".png",
        "declared_mime": "image/png",
        "document_type": document_type,
        "model_stage_eligible": True,
        "scenario_tags": tags,
        "expected_fields": fields,
        "expected_value_types": value_types,
        "expected_missing_fields": missing,
        "expected_evidence": evidence,
        "allowed_normalizations": {
            path: [value, value.upper(), f"  {value}  "]
            for path, value in fields.items()
            if path in {"merchant", "seller_name", "buyer_name"}
        },
        "expected_events": events,
        "expected_review_state": review_state,
        "render_lines": lines,
    }


def build_samples() -> list[dict[str, Any]]:
    payments = [
        payment_sample(
            "TUNE-PAY-001",
            merchant="Prompt Lab Cafe",
            currency="CNY",
            transaction_time="2026-08-01T09:15:00+08:00",
            timezone="Asia/Shanghai",
            amount_minor=8870,
            total_lines=["TOTAL: CNY 88.70"],
            amount_quote="CNY 88.70",
            optional={
                "payment_method": "Synthetic Wallet",
                "order_number": "DEV-PAY-0001",
                "category": "Meals",
            },
        ),
        payment_sample(
            "TUNE-PAY-002",
            merchant="Northwind Dev Store",
            currency="USD",
            transaction_time="2026-08-02T14:20:00-04:00",
            timezone="America/New_York",
            amount_minor=123456,
            total_lines=["TOTAL: USD 1,234.56"],
            amount_quote="USD 1,234.56",
        ),
        payment_sample(
            "TUNE-PAY-003",
            merchant="Euro Prompt Services",
            currency="EUR",
            transaction_time="2026-08-03T18:05:00+02:00",
            timezone="Europe/Berlin",
            amount_minor=123456,
            total_lines=["TOTAL: EUR 1.234,56"],
            amount_quote="EUR 1.234,56",
            optional={"order_number": "DEV-EU-0003", "category": "Software"},
        ),
        payment_sample(
            "TUNE-PAY-004",
            merchant="Tokyo Synthetic Kiosk",
            currency="JPY",
            transaction_time="2026-08-04T11:00:00+09:00",
            timezone="Asia/Tokyo",
            amount_minor=1200,
            total_lines=["TOTAL: JPY 1200"],
            amount_quote="JPY 1200",
            optional={"payment_method": "Test Card", "order_number": "DEV-JP-0004"},
        ),
        payment_sample(
            "TUNE-PAY-005",
            merchant="Missing Amount Sandbox",
            currency="CNY",
            transaction_time="2026-08-05T08:30:00+08:00",
            timezone="Asia/Shanghai",
            amount_minor=None,
            total_lines=["TOTAL: [MISSING]"],
            event="missing:amount_minor",
        ),
        payment_sample(
            "TUNE-PAY-006",
            merchant="Conflict Amount Sandbox",
            currency="USD",
            transaction_time="2026-08-06T16:45:00+00:00",
            timezone="UTC",
            amount_minor=None,
            total_lines=["TOTAL_A: USD 19.90", "TOTAL_B: USD 29.90"],
            event="conflict:amount_minor",
        ),
    ]

    invoices = [
        invoice_sample(
            "TUNE-INV-001",
            invoice_number="DEV-INV-CNY-0001",
            invoice_date="2026-07-11",
            currency="CNY",
            seller="Prompt Seller One",
            buyer="Prompt Buyer One",
            total_minor=11200,
            total_lines=["TOTAL: CNY 112.00"],
            total_quote="CNY 112.00",
            tax_minor=1200,
            tax_quote="CNY 12.00",
            items=[{
                "name": "Synthetic Consulting",
                "quantity": "2",
                "unit": "hour",
                "unit_price_minor": 5000,
                "amount_minor": 10000,
                "tax_minor": 1200,
                "sort_order": 0,
                "display": {
                    "unit_price_minor": "CNY 50.00",
                    "amount_minor": "CNY 100.00",
                    "tax_minor": "CNY 12.00",
                },
            }],
        ),
        invoice_sample(
            "TUNE-INV-002",
            invoice_number="DEV-INV-USD-0002",
            invoice_date="2026-07-12",
            currency="USD",
            seller="Northwind Prompt LLC",
            buyer="Contoso Dev Lab",
            total_minor=3500,
            total_lines=["TOTAL: USD 35.00"],
            total_quote="USD 35.00",
            tax_minor=None,
            tax_quote=None,
            items=[
                {
                    "name": "Synthetic Cable",
                    "quantity": "2",
                    "unit": "piece",
                    "unit_price_minor": 600,
                    "amount_minor": 1200,
                    "sort_order": 0,
                    "display": {"unit_price_minor": "USD 6.00", "amount_minor": "USD 12.00"},
                },
                {
                    "name": "Synthetic Adapter",
                    "quantity": "1",
                    "unit": "piece",
                    "unit_price_minor": 2300,
                    "amount_minor": 2300,
                    "sort_order": 1,
                    "display": {"unit_price_minor": "USD 23.00", "amount_minor": "USD 23.00"},
                },
            ],
        ),
        invoice_sample(
            "TUNE-INV-003",
            invoice_number="DEV-INV-EUR-0003",
            invoice_date="2026-07-13",
            currency="EUR",
            seller="Euro Prompt GmbH",
            buyer="Synthetic Buyer GmbH",
            total_minor=123456,
            total_lines=["TOTAL: EUR 1.234,56"],
            total_quote="EUR 1.234,56",
            tax_minor=23456,
            tax_quote="EUR 234,56",
            items=[{
                "name": "Synthetic Design Work",
                "quantity": "2.5",
                "unit": "hour",
                "unit_price_minor": 40000,
                "amount_minor": 100000,
                "tax_minor": 23456,
                "sort_order": 0,
                "display": {
                    "unit_price_minor": "EUR 400,00",
                    "amount_minor": "EUR 1.000,00",
                    "tax_minor": "EUR 234,56",
                },
            }],
        ),
        invoice_sample(
            "TUNE-INV-004",
            invoice_number="DEV-INV-JPY-0004",
            invoice_date="2026-07-14",
            currency="JPY",
            seller="Tokyo Prompt Works",
            buyer="Synthetic Japan Lab",
            total_minor=5000,
            total_lines=["TOTAL: JPY 5000"],
            total_quote="JPY 5000",
            tax_minor=None,
            tax_quote=None,
            items=[
                {"name": "Synthetic Part A", "amount_minor": 2000, "sort_order": 0, "display": {"amount_minor": "JPY 2000"}},
                {"name": "Synthetic Part B", "amount_minor": 3000, "sort_order": 1, "display": {"amount_minor": "JPY 3000"}},
            ],
        ),
        invoice_sample(
            "TUNE-INV-005",
            invoice_number=None,
            invoice_date="2026-07-15",
            currency="CNY",
            seller="Missing Number Seller",
            buyer="Missing Number Buyer",
            total_minor=1000,
            total_lines=["TOTAL: CNY 10.00"],
            total_quote="CNY 10.00",
            tax_minor=None,
            tax_quote=None,
            items=[{"name": "Synthetic Item", "amount_minor": 1000, "sort_order": 0, "display": {"amount_minor": "CNY 10.00"}}],
            event="missing:invoice_number",
        ),
        invoice_sample(
            "TUNE-INV-006",
            invoice_number="DEV-INV-CONFLICT-0006",
            invoice_date="2026-07-16",
            currency="USD",
            seller="Conflict Total Seller",
            buyer="Conflict Total Buyer",
            total_minor=None,
            total_lines=["TOTAL_A: USD 11.00", "TOTAL_B: USD 13.00"],
            total_quote=None,
            tax_minor=None,
            tax_quote=None,
            items=[{"name": "Synthetic Service", "amount_minor": 1100, "sort_order": 0, "display": {"amount_minor": "USD 11.00"}}],
            event="conflict:total_minor",
        ),
        invoice_sample(
            "TUNE-INV-007",
            invoice_number="DEV-INV-TYPE-0007",
            invoice_date="2026-07-17",
            currency="CNY",
            seller="Type Contract Seller",
            buyer="Type Contract Buyer",
            total_minor=1260,
            total_lines=["TOTAL: CNY 12.60"],
            total_quote="CNY 12.60",
            tax_minor=60,
            tax_quote="CNY 0.60",
            items=[{
                "name": "Synthetic Material",
                "quantity": "1.5",
                "unit": "kg",
                "unit_price_minor": 800,
                "amount_minor": 1200,
                "tax_minor": 60,
                "sort_order": 0,
                "display": {"unit_price_minor": "CNY 8.00", "amount_minor": "CNY 12.00", "tax_minor": "CNY 0.60"},
            }],
        ),
        invoice_sample(
            "TUNE-INV-008",
            invoice_number="DEV-INV-ORDER-0008",
            invoice_date="2026-07-18",
            currency="USD",
            seller="Sort Order Seller",
            buyer="Sort Order Buyer",
            total_minor=6000,
            total_lines=["TOTAL: USD 60.00"],
            total_quote="USD 60.00",
            tax_minor=None,
            tax_quote=None,
            items=[
                {"name": "Synthetic Row A", "amount_minor": 1000, "sort_order": 0, "display": {"amount_minor": "USD 10.00"}},
                {"name": "Synthetic Row B", "amount_minor": 2000, "sort_order": 1, "display": {"amount_minor": "USD 20.00"}},
                {"name": "Synthetic Row C", "amount_minor": 3000, "sort_order": 2, "display": {"amount_minor": "USD 30.00"}},
            ],
        ),
    ]

    unknowns = [
        unknown_sample("TUNE-UNK-001", ["SYNTHETIC SHIPPING MEMO", "REFERENCE: DEV-SHIP-0001", "NO PAYMENT OR INVOICE FIELDS"]),
        unknown_sample("TUNE-UNK-002", ["SYNTHETIC INVENTORY NOTE", "REFERENCE: DEV-STOCK-0002", "NO PAYMENT OR INVOICE FIELDS"]),
    ]
    return [*payments, *invoices, *unknowns]


def render_image(path: Path, lines: list[str]) -> None:
    image = Image.new("RGB", (960, 640), (247, 249, 252))
    draw = ImageDraw.Draw(image)
    draw.rounded_rectangle((18, 18, 942, 622), radius=18, fill=(255, 255, 255), outline=(206, 214, 224), width=2)
    font = ImageFont.truetype(str(FONT_PATH), 14)
    header_font = ImageFont.truetype(str(FONT_PATH), 15)
    y = 42
    for index, line in enumerate(lines):
        selected_font = header_font if index < 2 else font
        color = (20, 76, 148) if index == 1 else (27, 39, 56)
        draw.text((42, y), line, font=selected_font, fill=color)
        y += 34
    image.save(path, format="PNG", optimize=False, compress_level=9)
    os.chmod(path, 0o600)


def main() -> None:
    ensure_runtime()
    tuning_root = Path(__file__).resolve().parent.parent
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
        render_image(path, sample.pop("render_lines"))
        sample["file"] = f"assets/{DATASET_VERSION}/{name}"
        sample["sha256"] = sha256_bytes(path.read_bytes())
    for existing in asset_root.iterdir():
        if existing.is_file() and existing.name not in expected_names:
            raise RuntimeError(f"unlisted tuning asset exists: {existing.name}")

    manifest = {
        "dataset_version": DATASET_VERSION,
        "created_at": "2026-08-28T00:00:00Z",
        "synthetic_only": True,
        "intended_use": "prompt_provider_contract_tuning_only",
        "excluded_from_release_evidence": True,
        "source_dataset_versions": [],
        "generator": "tests/evaluation/tuning/generator/generate.py",
        "generator_runtime": {
            "python": PYTHON_VERSION,
            "pillow": PILLOW_VERSION,
            "font": str(FONT_PATH),
            "font_sha256": FONT_SHA256,
        },
        "samples": samples,
    }
    encoded = (json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode()
    manifest_path = tuning_root / "manifest-v1.json"
    manifest_path.write_bytes(encoded)
    os.chmod(manifest_path, 0o600)
    print(f"generated {len(samples)} independent tuning samples in {asset_root}")


if __name__ == "__main__":
    main()
