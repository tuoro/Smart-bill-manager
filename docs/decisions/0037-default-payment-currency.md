# ADR-0037：中文支付截图的默认币种

状态：已实现并本地验证；未发布
日期：2026-09-08

## 背景

`m1-real-dev-v5` 上 qwen3.8-flash 的真实预检显示，官方金额完全一致率只有 31.25%（5/16），而诊断分项同时给出 `payment_amount_visible_text_exact` 为 10/10：模型把十张支付截图的金额一字不差抄对了。压低指标的是币种。

同一份报告的 `payment_currency_labels_with_explicit_marker` 为 0/10。这十张微信/支付宝截图**本身就没有印任何币种标记**，金额文本里也没有符号。模型按 `bill-visible-text/2` 的约定对看不到的字段返回 `null`，是正确行为；`claim-mapper/4` 随后因为拿不到币种，把每个金额字段标记为 `money_currency_unavailable`，整份 Claim 阻断。

冻结的 `development-v5` 清单对这十份样本的 `currency` 期望值全部是 `CNY`。也就是说，在这条规则下管线永远不可能匹配上自己的冻结期望：数据集认定正确答案是 CNY，门禁要求它必须失败。

## 决定

`claim-mapper/4` 的支付区段在**显式币种字段与金额文本推导都拿不到币种**时，套用产品默认币种 `CNY`，且该字段不附 Evidence。

这与既有的 `defaultSourceTimezone = "Asia/Shanghai"` 是同一性质的产品默认值：它来自产品对适用场景的判断，不是票面逐字证据，因此不得写入 Evidence 伪装成 OCR 结果。

币种解析固定为三层，默认值只在最后兜底：

1. 模型显式返回 `currency` → 采用，保留其证据；
2. 否则从金额文本推导（`¥28.80`、`$100.00`）→ 采用，保留金额文本作为证据；
3. 两层都没有币种信号 → 产品默认 `CNY`，无 Evidence。

## 范围与边界

- 只作用于 `payment` 区段。发票不套用：中文发票均打印 `￥`，没有实证缺口，而给缺少标记的外币发票默认 `CNY` 是净增风险。
- 金额文本自带 `$`、`€`、`USD`、`美元` 等标记时在第 2 层命中，默认值走不到；若显式币种与金额文本冲突，`normalizeMoney` 的 `money_currency_conflict` 继续阻断。该性质由 `TestDefaultCurrencyNeverOverridesAnExplicitVisibleMarker` 固定。
- 本决定不放宽契约边界本身：退役的裸标量与非法页码仍然保持 blocked 字段。

## 与既有门禁的关系

`tests/critical-invariants.tsv` 的 `visible-text-contract-boundary` 原本要求「无币种金额保留为 blocked 字段」。该断言随本决定改写：契约形状与页码部分不变，无币种金额改为套用无证据的默认币种，并新增一行固定「默认币种不得覆盖票面显式外币标记」。

币种是 `score-model-evaluation.mjs` 里的支付关键字段，而关键字段证据覆盖率门槛为 100%、空证据必然判否。若不作处理，本决定会把该指标从 55/60 压到约 45/60——用一个 100% 门槛的指标换金额准确率，是不可接受的交换。

处理方式是复用清单已有的 `derived_field_provenance` 机制：计分器把来源标记为产品默认（`m1-payment-timezone/1`、`m1-payment-currency/1`）的关键字段排除出证据覆盖分母。理由是该指标衡量的是模型读数有没有票面支撑，产品默认值不是模型读数，放进分母度量不到任何东西；时区正因同样的原因从未进入关键字段列表。豁免只认这两个批准标识，漏标或写成其他字符串一律照常计入，由 `score-model-evaluation.test.mjs` 固定。

## 已知风险

一张没有任何币种标记的外币支付截图会被记为 CNY。缓解只有两点：票面出现任何币种标记即不走默认；用户在审核台确认 Fact 前可以看到并修改币种字段。若后续出现真实的无标记外币样本，应重新评估是否改为在审核台要求用户显式确认币种。

## 实测更正（2026-09-08）

首次实现遗漏了领域校验：`validation.go` 的 `requiresEvidence` 只豁免 `source_timezone` 与 `sort_order`，因此无证据的默认币种被 `missing_field_evidence` 直接判 blocked，真实预检里出现 9 次。写这份决定时只查了 claim-mapper 与计分器，没查领域校验，这是漏算。

修正是把 `currency` 一并纳入该豁免。模型返回的币种始终经 `mapVisibleField` 生成证据，因此这条豁免在实际链路上不会放过模型输出；代价是路径级豁免比按来源判定粗，若将来币种可能来自无证据的其它渠道，需要改为显式来源标记。

同一轮预检还暴露出与本决定无关的更大障碍：支付金额的前导负号，见 [ADR-0038](0038-payment-amount-direction-sign.md)。币种默认只是解开第一道，两道都通才能得到可组装的 Claim。

## 待验证

本决定尚未在真实样本上重跑。预期 `amount_exact_rate` 从 5/16 回到接近 15/16，但这只是推算；实际幅度必须由对齐到活动契约后的评测给出，不得在此之前写成已达成。
