import assert from "node:assert/strict";
import test from "node:test";

import {
  criticalFields,
  productDefaultProvenance,
  scoreRun,
} from "./score-model-evaluation.mjs";

function paymentSample(provenance) {
  return {
    sample_id: "S-1",
    document_type: "payment",
    model_stage_eligible: true,
    expected_review_state: "needs_review",
    expected_fields: {
      amount_minor: 2880,
      currency: "CNY",
      merchant: "合成商户",
      transaction_time: "2026-08-29T14:35:00+08:00",
    },
    expected_missing_fields: [],
    expected_evidence: {
      amount_minor: { page: 1, quote: "28.80" },
      currency: { page: 1, quote: "28.80" },
      merchant: { page: 1, quote: "合成商户" },
      transaction_time: { page: 1, quote: "2026年8月29日 14:35" },
    },
    derived_field_provenance: provenance,
    expected_events: [],
  };
}

// 默认币种没有票面证据，本地按产品规则确定。它进入分母只会逼出伪造证据，
// 所以按 derived_field_provenance 排除；其余关键字段照常计入。见 ADR-0037。
const observedRun = {
  run_id: "test",
  samples: [
    {
      sample_id: "S-1",
      outcome: "local_claim_accepted",
      schema_valid: true,
      job_status: "needs_review",
      claim: {
        document_type: "payment",
        claim_status: "needs_review",
        fields: [
          {
            path: "amount_minor",
            presence: "present",
            value: 2880,
            value_type: "money_minor",
            evidence: [{ page: 1, quote: "28.80" }],
          },
          {
            path: "currency",
            presence: "present",
            value: "CNY",
            value_type: "string",
            evidence: [],
          },
          {
            path: "merchant",
            presence: "present",
            value: "合成商户",
            value_type: "string",
            evidence: [{ page: 1, quote: "合成商户" }],
          },
          {
            path: "transaction_time",
            presence: "present",
            value: "2026-08-29T14:35:00+08:00",
            value_type: "instant",
            evidence: [{ page: 1, quote: "2026年8月29日 14:35" }],
          },
        ],
      },
    },
  ],
};

test("product-default fields stay out of the critical evidence denominator", () => {
  const excluded = scoreRun(
    { samples: [paymentSample({ currency: "m1-payment-currency/1" })] },
    observedRun,
  ).metrics.critical_evidence_coverage;
  assert.equal(excluded.denominator, 3);
  assert.equal(excluded.percentage, 100);

  // 没有标记来源时不豁免：空证据仍然如实计为未覆盖，不能靠漏标蒙混过关。
  const counted = scoreRun({ samples: [paymentSample({})] }, observedRun)
    .metrics.critical_evidence_coverage;
  assert.equal(counted.denominator, 4);
  assert.equal(counted.numerator, 3);
});

test("only the approved provenance identifiers grant the exemption", () => {
  const bogus = scoreRun(
    {
      samples: [
        paymentSample({
          currency: "model_semantic_normalization_from_chinese_payment_context",
        }),
      ],
    },
    observedRun,
  ).metrics.critical_evidence_coverage;
  assert.equal(bogus.denominator, 4);
  assert.ok(productDefaultProvenance.has("m1-payment-timezone/1"));
  assert.ok(criticalFields.payment.includes("currency"));
});

// 名称与契约比对、证据匹配必须用同一条规范化规则。此前名称指标误用了不含括号
// 规则的 normalizeExact，导致「（某某公司）」与「某某公司」在契约比对里判相等、
// 在名称指标里判不等，同一条 acceptance 规则在同一文件里出现两种口径。
test("name comparison applies the same enclosing-parenthesis rule as the rest", () => {
  const sample = {
    sample_id: "S-1",
    document_type: "payment",
    model_stage_eligible: true,
    expected_review_state: "needs_review",
    expected_fields: { merchant: "合成商户" },
    expected_missing_fields: [],
    expected_evidence: { merchant: { page: 1, quote: "合成商户" } },
    derived_field_provenance: {},
    expected_events: [],
  };
  const observed = (value) => ({
    run_id: "test",
    samples: [
      {
        sample_id: "S-1",
        outcome: "local_claim_accepted",
        schema_valid: true,
        job_status: "needs_review",
        claim: {
          document_type: "payment",
          claim_status: "needs_review",
          fields: [
            {
              path: "merchant",
              presence: "present",
              value,
              value_type: "string",
              evidence: [{ page: 1, quote: value }],
            },
          ],
        },
      },
    ],
  });
  for (const value of [
    "合成商户",
    "（合成商户）",
    "(合成商户)",
    " 合成商户 ",
  ]) {
    const metric = scoreRun({ samples: [sample] }, observed(value)).metrics
      .name_normalization_exact_rate;
    assert.equal(metric.numerator, 1, `${value} 应判为一致`);
  }
  // 括号只在包住整个值时等价，内部括号仍是值的一部分。
  const inner = scoreRun({ samples: [sample] }, observed("合成（商户）"))
    .metrics.name_normalization_exact_rate;
  assert.equal(inner.numerator, 0);
});
