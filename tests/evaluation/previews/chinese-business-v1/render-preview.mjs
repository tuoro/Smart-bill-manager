#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "../../../../apps/web/node_modules/playwright/index.mjs";

const outputDirectory = dirname(fileURLToPath(import.meta.url));

const samples = [
  {
    sampleId: "CN-PREVIEW-PAY-001",
    file: "cn-pay-mobile-clean.png",
    viewport: { width: 750, height: 1624 },
    documentType: "payment",
    scenarioTags: ["中文支付截图", "清晰", "单一主金额"],
    expectedFields: {
      amount_minor: 4860,
      currency: "CNY",
      merchant: "春山面馆（合成）",
      transaction_time: "2026-08-18T12:36:42+08:00",
      source_timezone: "Asia/Shanghai",
      payment_method: "招商银行储蓄卡（2048）",
      order_number: "202608182104009731",
    },
    expectedEvidence: {
      amount_minor: "¥48.60",
      merchant: "春山面馆（合成）",
      transaction_time: "2026年8月18日 12:36:42",
      payment_method: "招商银行储蓄卡（2048）",
      order_number: "202608182104009731",
    },
    render: renderMobilePayment,
  },
  {
    sampleId: "CN-PREVIEW-PAY-002",
    file: "cn-pay-order-multiple-amounts.png",
    viewport: { width: 750, height: 1624 },
    documentType: "payment",
    scenarioTags: ["中文订单截图", "多金额", "优惠抵扣"],
    expectedFields: {
      amount_minor: 26900,
      currency: "CNY",
      merchant: "北岸生活馆（合成）",
      transaction_time: "2026-08-20T20:18:09+08:00",
      source_timezone: "Asia/Shanghai",
      payment_method: "账户余额",
      order_number: "CN202608202018090027",
    },
    expectedEvidence: {
      amount_minor: "实付款 ¥269.00",
      merchant: "北岸生活馆（合成）",
      transaction_time: "2026-08-20 20:18:09",
      payment_method: "账户余额",
      order_number: "CN202608202018090027",
    },
    render: renderOrderPayment,
  },
  {
    sampleId: "CN-PREVIEW-INV-001",
    file: "cn-digital-invoice-clean.png",
    viewport: { width: 1600, height: 1080 },
    documentType: "invoice",
    scenarioTags: ["中文数电发票", "清晰", "单项目"],
    expectedFields: {
      invoice_number: "25312000000084273115",
      invoice_date: "2026-08-16",
      total_minor: 12800,
      tax_minor: 725,
      currency: "CNY",
      seller_name: "上海澄明软件技术有限公司（合成）",
      buyer_name: "杭州远舟信息服务有限公司（合成）",
    },
    expectedItems: [
      {
        name: "软件技术服务",
        quantity: "1",
        unit: "项",
        unit_price_minor: 12075,
        amount_minor: 12075,
        tax_minor: 725,
        sort_order: 0,
      },
    ],
    expectedEvidence: {
      invoice_number: "发票号码：25312000000084273115",
      invoice_date: "开票日期：2026年08月16日",
      total_minor: "¥128.00",
      tax_minor: "¥7.25",
      seller_name: "上海澄明软件技术有限公司（合成）",
      buyer_name: "杭州远舟信息服务有限公司（合成）",
    },
    render: renderDigitalInvoice,
  },
  {
    sampleId: "CN-PREVIEW-PAY-003",
    file: "cn-thermal-receipt-photo.png",
    viewport: { width: 900, height: 1400 },
    documentType: "payment",
    scenarioTags: ["中文热敏小票", "拍照", "多金额"],
    expectedFields: {
      amount_minor: 3240,
      currency: "CNY",
      merchant: "南巷便利店（合成）",
      transaction_time: "2026-08-22T19:08:16+08:00",
      source_timezone: "Asia/Shanghai",
      payment_method: "移动支付",
      order_number: "TX20260822190816083",
    },
    expectedEvidence: {
      amount_minor: "实收：¥32.40",
      merchant: "南巷便利店（合成）",
      transaction_time: "2026-08-22 19:08:16",
      payment_method: "支付方式：移动支付",
      order_number: "流水号：TX20260822190816083",
    },
    render: renderReceiptPhoto,
  },
];

function baseHtml(content, extraStyles = "") {
  return `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8">
    <meta name="color-scheme" content="light">
    <style>
      * { box-sizing: border-box; }
      html, body { width: 100%; height: 100%; margin: 0; }
      body {
        overflow: hidden;
        color: #172033;
        font-family: "Noto Sans CJK SC", "Microsoft YaHei", sans-serif;
        text-rendering: geometricPrecision;
        -webkit-font-smoothing: antialiased;
      }
      .synthetic-badge {
        position: absolute;
        z-index: 20;
        top: 22px;
        right: 22px;
        padding: 7px 14px;
        border: 1px solid rgba(173, 42, 42, .28);
        border-radius: 999px;
        color: #9b2929;
        background: rgba(255, 244, 244, .92);
        font-size: 15px;
        font-weight: 700;
        letter-spacing: .08em;
      }
      ${extraStyles}
    </style>
  </head>
  <body>${content}</body>
</html>`;
}

function chevronLeft() {
  return `<svg width="28" height="28" viewBox="0 0 24 24" aria-hidden="true"><path d="m15 18-6-6 6-6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
}

function dotsIcon() {
  return `<svg width="30" height="30" viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1.4" fill="currentColor"/><circle cx="12" cy="12" r="1.4" fill="currentColor"/><circle cx="19" cy="12" r="1.4" fill="currentColor"/></svg>`;
}

function shieldIcon() {
  return `<svg width="44" height="44" viewBox="0 0 48 48" aria-hidden="true"><path d="M24 5 39 11v11c0 9.7-6.1 17.5-15 21-8.9-3.5-15-11.3-15-21V11L24 5Z" fill="#e8f7f1"/><path d="m17.5 23.5 4.2 4.1 8.9-9.2" fill="none" stroke="#148561" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
}

function phoneChrome(title) {
  return `<div class="statusbar"><span>12:37</span><div class="status-icons"><span>▮▮▮</span><span>⌁</span><span class="battery">86</span></div></div>
    <header class="mobile-nav"><span>${chevronLeft()}</span><strong>${title}</strong><span>${dotsIcon()}</span></header>`;
}

function mobileStyles(accent = "#148561") {
  return `
    body { background: #edf1f5; }
    .phone { position: relative; width: 100%; height: 100%; background: #f5f7f8; }
    .phone > .synthetic-badge { top: auto; right: 22px; bottom: 22px; }
    .statusbar { height: 52px; padding: 15px 30px 0; display: flex; justify-content: space-between; align-items: flex-start; background: white; font-size: 21px; font-weight: 650; }
    .status-icons { display: flex; gap: 12px; align-items: center; font-size: 15px; letter-spacing: -2px; }
    .battery { min-width: 38px; padding: 2px 6px; border: 2px solid #18202d; border-radius: 6px; font-size: 13px; letter-spacing: 0; text-align: center; }
    .mobile-nav { height: 88px; padding: 0 28px; display: grid; grid-template-columns: 64px 1fr 64px; align-items: center; background: white; color: #18202d; }
    .mobile-nav strong { text-align: center; font-size: 30px; font-weight: 650; }
    .mobile-nav > span:last-child { justify-self: end; }
    .hero { padding: 54px 40px 50px; text-align: center; background: white; border-bottom: 1px solid #edf0f2; }
    .merchant-avatar { width: 78px; height: 78px; margin: 0 auto 20px; display: grid; place-items: center; border-radius: 24px; color: white; background: linear-gradient(145deg, ${accent}, #2fb58a); box-shadow: 0 12px 28px rgba(20,133,97,.2); font-size: 34px; font-weight: 800; }
    .merchant-name { color: #2b3442; font-size: 26px; font-weight: 620; }
    .amount { margin-top: 18px; font-size: 68px; font-weight: 750; line-height: 1.1; letter-spacing: -2px; font-variant-numeric: tabular-nums; }
    .amount .currency { margin-right: 6px; font-size: 38px; vertical-align: 12px; }
    .success { margin-top: 18px; display: inline-flex; gap: 10px; align-items: center; color: ${accent}; font-size: 24px; font-weight: 650; }
    .detail-card { margin: 24px 22px; padding: 8px 30px; border-radius: 22px; background: white; box-shadow: 0 5px 24px rgba(24,32,45,.05); }
    .detail-row { min-height: 86px; display: grid; grid-template-columns: 190px 1fr; align-items: center; border-bottom: 1px solid #edf0f2; font-size: 23px; }
    .detail-row:last-child { border-bottom: 0; }
    .detail-row .label { color: #8a929e; }
    .detail-row .value { color: #26303d; text-align: right; font-weight: 540; overflow-wrap: anywhere; }
    .detail-row .value.mono { font-family: "Noto Sans Mono CJK SC", monospace; font-size: 21px; }
    .notice { margin: 32px 42px 0; display: flex; gap: 14px; align-items: flex-start; color: #77808e; font-size: 20px; line-height: 1.7; }
    .notice svg { flex: 0 0 auto; }
  `;
}

function renderMobilePayment() {
  return baseHtml(
    `<main class="phone">
      <span class="synthetic-badge">合成样本 · 非真实交易</span>
      ${phoneChrome("账单详情")}
      <section class="hero">
        <div class="merchant-avatar">春</div>
        <div class="merchant-name">春山面馆（合成）</div>
        <div class="amount"><span class="currency">¥</span>48.60</div>
        <div class="success">${shieldIcon()}<span>支付成功</span></div>
      </section>
      <section class="detail-card">
        <div class="detail-row"><span class="label">当前状态</span><span class="value">支付成功</span></div>
        <div class="detail-row"><span class="label">支付时间</span><span class="value">2026年8月18日 12:36:42</span></div>
        <div class="detail-row"><span class="label">支付方式</span><span class="value">招商银行储蓄卡（2048）</span></div>
        <div class="detail-row"><span class="label">商品说明</span><span class="value">堂食消费</span></div>
        <div class="detail-row"><span class="label">商户全称</span><span class="value">春山面馆（合成）</span></div>
        <div class="detail-row"><span class="label">交易单号</span><span class="value mono">202608182104009731</span></div>
      </section>
      <div class="notice">${shieldIcon()}<span>该画面为 Smart Bill Manager 的纯合成测试资产，不对应任何真实账户、商户或资金交易。</span></div>
    </main>`,
    mobileStyles(),
  );
}

function renderOrderPayment() {
  return baseHtml(
    `<main class="phone order-phone">
      <span class="synthetic-badge">合成样本 · 非真实订单</span>
      ${phoneChrome("订单支付详情")}
      <section class="order-status">
        <div class="order-check">✓</div>
        <div><strong>支付完成</strong><p>商家正在准备您的商品</p></div>
      </section>
      <section class="shop-card">
        <div class="shop-title"><span class="shop-mark">北</span><strong>北岸生活馆（合成）</strong><span>›</span></div>
        <div class="product">
          <div class="product-image"><span>合成</span></div>
          <div class="product-copy"><strong>轻量通勤双肩包 · 雾灰色</strong><p>规格：标准版 / 1 件</p><span>支持七天无理由</span></div>
          <div class="product-price">¥319.00</div>
        </div>
      </section>
      <section class="price-card">
        <div><span>商品金额</span><b>¥319.00</b></div>
        <div><span>店铺优惠</span><b class="discount">-¥30.00</b></div>
        <div><span>平台红包</span><b class="discount">-¥20.00</b></div>
        <div><span>配送费</span><b>¥0.00</b></div>
        <div class="paid"><span>实付款</span><b>¥269.00</b></div>
      </section>
      <section class="order-detail">
        <div><span>订单编号</span><b>CN202608202018090027</b></div>
        <div><span>支付方式</span><b>账户余额</b></div>
        <div><span>支付时间</span><b>2026-08-20 20:18:09</b></div>
      </section>
      <footer>本页为纯合成测试样本 · 金额与商户均为虚构</footer>
    </main>`,
    `${mobileStyles("#1769e0")}
      .order-phone { background: #f3f5f8; }
      .order-status { height: 176px; padding: 34px 42px; display: flex; gap: 24px; align-items: center; color: white; background: linear-gradient(125deg, #1769e0, #408afb); }
      .order-check { width: 70px; height: 70px; display: grid; place-items: center; border-radius: 50%; background: rgba(255,255,255,.2); border: 2px solid rgba(255,255,255,.65); font-size: 40px; }
      .order-status strong { font-size: 32px; }
      .order-status p { margin: 8px 0 0; opacity: .82; font-size: 21px; }
      .shop-card, .price-card, .order-detail { margin: 22px; padding: 0 28px; border-radius: 22px; background: white; box-shadow: 0 5px 24px rgba(24,32,45,.045); }
      .shop-title { height: 84px; display: flex; gap: 15px; align-items: center; border-bottom: 1px solid #edf0f2; font-size: 23px; }
      .shop-title > span:last-child { margin-left: auto; color: #a1a8b2; font-size: 34px; }
      .shop-mark { width: 40px; height: 40px; display: grid; place-items: center; border-radius: 11px; color: white; background: #1769e0; font-weight: 750; }
      .product { min-height: 184px; padding: 24px 0; display: grid; grid-template-columns: 124px 1fr auto; gap: 20px; }
      .product-image { display: grid; place-items: center; border-radius: 16px; color: #687386; background: linear-gradient(145deg,#e9edf2,#cfd7e2); font-weight: 750; }
      .product-copy strong { font-size: 22px; line-height: 1.5; }
      .product-copy p { margin: 9px 0; color: #929aa6; font-size: 19px; }
      .product-copy span { padding: 4px 8px; border-radius: 5px; color: #1769e0; background: #eef5ff; font-size: 16px; }
      .product-price { font-size: 20px; font-weight: 700; }
      .price-card { padding-top: 14px; padding-bottom: 14px; }
      .price-card > div, .order-detail > div { min-height: 62px; display: flex; align-items: center; justify-content: space-between; color: #5e6876; font-size: 21px; }
      .price-card b, .order-detail b { color: #293342; font-weight: 560; }
      .price-card .discount { color: #e5573e; }
      .price-card .paid { min-height: 82px; margin-top: 5px; border-top: 1px solid #e9edf1; color: #1d2735; font-weight: 650; }
      .price-card .paid b { color: #e34d32; font-size: 34px; font-weight: 780; }
      .order-detail { padding-top: 12px; padding-bottom: 12px; }
      .order-detail b { max-width: 430px; text-align: right; font-family: "Noto Sans Mono CJK SC", monospace; font-size: 19px; }
      footer { padding-top: 10px; color: #9aa2ad; text-align: center; font-size: 17px; }
    `,
  );
}

function mockQr() {
  const size = 25;
  const modules = [];
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      const inFinder =
        (x < 7 && y < 7) ||
        (x >= size - 7 && y < 7) ||
        (x < 7 && y >= size - 7);
      const finderOn =
        inFinder &&
        (x % (size - 7) === 0 ||
          y % (size - 7) === 0 ||
          x % (size - 7) === 6 ||
          y % (size - 7) === 6 ||
          (x % (size - 7) >= 2 && x % (size - 7) <= 4 && y % (size - 7) >= 2 && y % (size - 7) <= 4));
      const dataOn = !inFinder && ((x * 11 + y * 7 + x * y) % 9 < 4);
      if (finderOn || dataOn) {
        modules.push(`<rect x="${x}" y="${y}" width="1" height="1"/>`);
      }
    }
  }
  return `<svg class="qr" viewBox="0 0 ${size} ${size}" role="img" aria-label="不可扫描的合成二维码">${modules.join("")}</svg>`;
}

function renderDigitalInvoice() {
  return baseHtml(
    `<main class="invoice-stage">
      <article class="invoice-paper">
        <div class="invoice-watermark">合成测试票据</div>
        <span class="synthetic-badge">纯合成 · 不可报销</span>
        <header class="invoice-header">
          <div class="qr-wrap">${mockQr()}<small>仅作版式占位<br>不可扫描</small></div>
          <div class="invoice-title"><span>电子发票</span><strong>（普通发票）</strong><i></i></div>
          <div class="invoice-meta"><div><span>发票号码：</span><b>25312000000084273115</b></div><div><span>开票日期：</span><b>2026年08月16日</b></div></div>
        </header>
        <section class="party-grid">
          <div class="party-label">购<br>买<br>方<br>信<br>息</div>
          <div class="party-info"><p><span>名称：</span><b>杭州远舟信息服务有限公司（合成）</b></p><p><span>统一社会信用代码/纳税人识别号：</span><b>91330100SYNTH0001X</b></p></div>
          <div class="party-label">销<br>售<br>方<br>信<br>息</div>
          <div class="party-info"><p><span>名称：</span><b>上海澄明软件技术有限公司（合成）</b></p><p><span>统一社会信用代码/纳税人识别号：</span><b>91310100SYNTH0002Y</b></p></div>
        </section>
        <section class="invoice-table">
          <div class="invoice-row table-head"><span>项目名称</span><span>规格型号</span><span>单位</span><span>数量</span><span>单价</span><span>金额</span><span>税率/征收率</span><span>税额</span></div>
          <div class="invoice-row table-item"><span>*信息技术服务*软件技术服务</span><span>标准服务</span><span>项</span><span>1</span><span>120.75</span><span>120.75</span><span>6%</span><span>7.25</span></div>
          <div class="invoice-row table-space"><span></span><span></span><span></span><span></span><span></span><span></span><span></span><span></span></div>
          <div class="invoice-row table-total"><span>合　　计</span><span></span><span></span><span></span><span></span><span>¥120.75</span><span></span><span>¥7.25</span></div>
        </section>
        <section class="grand-total"><span>价税合计（大写）</span><strong>壹佰贰拾捌圆整</strong><span>（小写）</span><b>¥128.00</b></section>
        <section class="invoice-bottom">
          <div class="remarks"><span>备　注</span><p>本发票为 Smart Bill Manager 纯合成测试资产，不对应真实交易，不具备任何报销或税务效力。</p></div>
          <div class="issuer"><span>开票人：</span><b>测试开票员</b></div>
          <div class="seal"><span>合成测试</span><small>不可报销</small></div>
        </section>
        <footer>下载次数：1　　版式文件生成时间：2026-08-16 10:24:08　　币种：人民币（CNY）</footer>
      </article>
    </main>`,
    `
      body { background: #dfe5eb; }
      .invoice-stage { width: 100%; height: 100%; padding: 70px; display: grid; place-items: center; background: radial-gradient(circle at 50% 10%,#f7f9fb,#dfe5eb 70%); }
      .invoice-paper { position: relative; width: 1460px; height: 900px; padding: 54px 58px 34px; overflow: hidden; color: #26364b; background: #fff; border: 1px solid #c8d3df; box-shadow: 0 28px 70px rgba(32,47,66,.18); font-family: "Noto Serif CJK SC", serif; }
      .invoice-paper::before { content: ""; position: absolute; inset: 0; pointer-events: none; opacity: .24; background-image: radial-gradient(#8ca4bb 0.45px,transparent .55px); background-size: 8px 8px; }
      .invoice-watermark { position: absolute; z-index: 0; top: 390px; left: 455px; transform: rotate(-18deg); color: rgba(184,37,37,.065); font-size: 96px; font-weight: 800; letter-spacing: .18em; white-space: nowrap; }
      .invoice-header { position: relative; height: 170px; display: grid; grid-template-columns: 250px 1fr 390px; align-items: start; }
      .qr-wrap { display: flex; gap: 16px; align-items: end; }
      .qr { width: 118px; height: 118px; fill: #25364a; image-rendering: pixelated; }
      .qr-wrap small { padding-bottom: 8px; color: #728096; font-family: "Noto Sans CJK SC", sans-serif; font-size: 15px; line-height: 1.5; }
      .invoice-title { padding-top: 2px; text-align: center; color: #9c1e24; }
      .invoice-title span { display: block; font-size: 44px; font-weight: 720; letter-spacing: .24em; }
      .invoice-title strong { display: block; margin-top: 3px; font-size: 24px; letter-spacing: .1em; }
      .invoice-title i { display: block; width: 390px; height: 3px; margin: 13px auto 0; background: #a41f25; box-shadow: 0 6px 0 #a41f25; }
      .invoice-meta { padding-top: 24px; font-family: "Noto Sans CJK SC", sans-serif; font-size: 20px; }
      .invoice-meta div { display: flex; margin-bottom: 15px; }
      .invoice-meta span { width: 112px; color: #46566b; }
      .invoice-meta b { color: #172943; font-family: "Noto Sans Mono CJK SC", monospace; font-weight: 600; }
      .party-grid { position: relative; display: grid; grid-template-columns: 54px 1fr 54px 1fr; min-height: 137px; border: 2px solid #6a90af; }
      .party-label { display: grid; place-items: center; padding: 6px 0; color: #315c7e; background: #f3f8fb; border-right: 1px solid #87a7bf; font-size: 17px; line-height: 1.2; font-weight: 650; }
      .party-info { padding: 19px 24px; border-right: 1px solid #87a7bf; font-family: "Noto Sans CJK SC", sans-serif; }
      .party-info:last-child { border-right: 0; }
      .party-info p { margin: 0 0 15px; display: flex; font-size: 18px; }
      .party-info p:last-child { margin-bottom: 0; }
      .party-info span { flex: 0 0 auto; color: #4e6175; }
      .party-info b { font-weight: 570; }
      .invoice-table { position: relative; border-right: 2px solid #6a90af; border-left: 2px solid #6a90af; font-family: "Noto Sans CJK SC", sans-serif; }
      .invoice-row { display: grid; grid-template-columns: 2.7fr 1.2fr .65fr .65fr 1fr 1fr 1.15fr 1fr; border-bottom: 1px solid #87a7bf; }
      .invoice-row span { min-width: 0; padding: 9px 7px; display: grid; place-items: center; border-right: 1px solid #a6bdcf; text-align: center; }
      .invoice-row span:last-child { border-right: 0; }
      .table-head { color: #315c7e; background: #f3f8fb; font-size: 17px; font-weight: 650; }
      .table-item { min-height: 62px; font-size: 17px; }
      .table-space { height: 76px; }
      .table-total { min-height: 48px; font-size: 18px; font-weight: 620; }
      .grand-total { position: relative; height: 72px; padding: 0 24px; display: grid; grid-template-columns: 190px 1fr 100px 200px; align-items: center; border: 2px solid #6a90af; border-top: 0; font-size: 20px; }
      .grand-total span { color: #315c7e; }
      .grand-total strong { font-size: 22px; letter-spacing: .16em; }
      .grand-total b { color: #162c47; font-family: "Noto Sans Mono CJK SC", monospace; font-size: 25px; }
      .invoice-bottom { position: relative; height: 124px; display: grid; grid-template-columns: 1fr 270px; border: 2px solid #6a90af; border-top: 0; font-family: "Noto Sans CJK SC", sans-serif; }
      .remarks { display: grid; grid-template-columns: 86px 1fr; border-right: 1px solid #87a7bf; }
      .remarks > span { display: grid; place-items: center; color: #315c7e; background: #f3f8fb; border-right: 1px solid #87a7bf; font-size: 18px; }
      .remarks p { margin: 0; padding: 22px; color: #526275; font-size: 17px; line-height: 1.7; }
      .issuer { padding: 18px 126px 18px 20px; display: flex; flex-direction: column; gap: 8px; font-size: 17px; }
      .issuer span { color: #526275; }
      .seal { position: absolute; right: 10px; bottom: 5px; width: 108px; height: 108px; display: grid; place-content: center; border: 5px double rgba(183,32,36,.72); border-radius: 50%; transform: rotate(-11deg); color: rgba(183,32,36,.78); text-align: center; }
      .seal span { font-size: 19px; font-weight: 750; letter-spacing: .13em; }
      .seal small { margin-top: 5px; font-size: 14px; font-weight: 650; }
      .invoice-paper footer { position: relative; padding-top: 14px; color: #617187; text-align: right; font-family: "Noto Sans CJK SC", sans-serif; font-size: 14px; }
    `,
  );
}

function renderReceiptPhoto() {
  return baseHtml(
    `<main class="photo-stage">
      <span class="synthetic-badge">合成样本 · 模拟拍照</span>
      <div class="paper-shadow"></div>
      <article class="receipt">
        <div class="receipt-noise"></div>
        <header><strong>南巷便利店（合成）</strong><span>SYNTHETIC RECEIPT / 非真实交易</span></header>
        <div class="dash"></div>
        <section class="receipt-meta"><div><span>门店：</span><b>滨江测试店 01</b></div><div><span>流水号：</span><b>TX20260822190816083</b></div><div><span>时间：</span><b>2026-08-22 19:08:16</b></div></section>
        <div class="dash"></div>
        <section class="receipt-items">
          <div class="item-head"><span>商品</span><span>数量</span><span>金额</span></div>
          <div><span>茉莉绿茶 500ml</span><span>2</span><span>11.80</span></div>
          <div><span>鲜奶吐司</span><span>1</span><span>12.60</span></div>
          <div><span>原味酸奶</span><span>1</span><span>11.00</span></div>
        </section>
        <div class="dash"></div>
        <section class="receipt-summary"><div><span>商品小计</span><b>¥35.40</b></div><div><span>会员优惠</span><b>-¥3.00</b></div><div class="received"><span>实收</span><b>¥32.40</b></div></section>
        <div class="dash"></div>
        <section class="receipt-pay"><div>支付方式：移动支付</div><div>币种：人民币 CNY</div></section>
        <div class="barcode"><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i><i></i></div>
        <footer><strong>谢谢惠顾</strong><span>此小票为纯合成测试资产</span></footer>
      </article>
    </main>`,
    `
      body { background: #a9815e; }
      .photo-stage { position: relative; width: 100%; height: 100%; overflow: hidden; background: repeating-linear-gradient(92deg, rgba(255,255,255,.026) 0 2px, transparent 2px 83px), linear-gradient(135deg,#b89471,#886246); }
      .photo-stage::after { content: ""; position: absolute; inset: 0; pointer-events: none; background: radial-gradient(circle at 48% 42%, transparent 28%, rgba(43,25,15,.24) 100%); }
      .paper-shadow { position: absolute; top: 90px; left: 145px; width: 615px; height: 1225px; transform: rotate(1.8deg); background: rgba(43,27,15,.35); filter: blur(28px); }
      .receipt { position: absolute; z-index: 2; top: 70px; left: 135px; width: 620px; min-height: 1245px; padding: 60px 52px 54px; overflow: hidden; transform: rotate(-1.25deg); color: #20201e; background: #f8f4e9; box-shadow: 0 20px 60px rgba(38,23,15,.28); font-family: "Noto Sans Mono CJK SC", monospace; }
      .receipt::before, .receipt::after { content: ""; position: absolute; left: -8px; right: -8px; height: 18px; background: radial-gradient(circle at 9px 0,#a9815e 7px,transparent 8px) 0 0/18px 18px repeat-x; }
      .receipt::before { top: -1px; transform: rotate(180deg); }
      .receipt::after { bottom: -1px; }
      .receipt-noise { position: absolute; inset: 0; pointer-events: none; opacity: .16; mix-blend-mode: multiply; background-image: repeating-linear-gradient(0deg,transparent 0 3px,rgba(60,60,55,.07) 3px 4px); }
      .receipt header { position: relative; display: flex; flex-direction: column; align-items: center; text-align: center; }
      .receipt header strong { font-size: 31px; letter-spacing: .11em; }
      .receipt header span { margin-top: 14px; color: #66645f; font-size: 16px; letter-spacing: .06em; }
      .dash { position: relative; height: 1px; margin: 31px 0; border-top: 2px dashed #67655f; opacity: .72; }
      .receipt-meta div { min-height: 41px; display: grid; grid-template-columns: 100px 1fr; align-items: center; font-size: 20px; }
      .receipt-meta span { color: #55534e; }
      .receipt-meta b { overflow-wrap: anywhere; font-weight: 520; }
      .receipt-items > div { min-height: 54px; display: grid; grid-template-columns: 1fr 70px 100px; align-items: center; font-size: 20px; }
      .receipt-items span:nth-child(2), .receipt-items span:nth-child(3) { text-align: right; }
      .receipt-items .item-head { color: #55534e; border-bottom: 1px solid rgba(68,67,63,.35); font-weight: 650; }
      .receipt-summary div { min-height: 50px; display: flex; align-items: center; justify-content: space-between; font-size: 21px; }
      .receipt-summary b { font-weight: 580; }
      .receipt-summary .received { min-height: 76px; margin-top: 8px; padding-top: 12px; border-top: 2px solid #55534e; font-size: 27px; font-weight: 750; }
      .receipt-summary .received b { font-size: 36px; }
      .receipt-pay { font-size: 20px; line-height: 1.9; }
      .barcode { height: 76px; margin: 38px 40px 14px; display: flex; align-items: stretch; justify-content: center; gap: 5px; }
      .barcode i { width: 4px; background: #262624; }
      .barcode i:nth-child(3n) { width: 9px; }
      .barcode i:nth-child(4n) { width: 2px; }
      .barcode i:nth-child(5n) { margin-right: 8px; }
      .receipt footer { display: flex; flex-direction: column; align-items: center; gap: 10px; text-align: center; }
      .receipt footer strong { font-size: 25px; letter-spacing: .28em; }
      .receipt footer span { color: #66645f; font-size: 16px; }
    `,
  );
}

async function sha256File(path) {
  const bytes = await readFile(path);
  return createHash("sha256").update(bytes).digest("hex");
}

await mkdir(outputDirectory, { recursive: true });
const browser = await chromium.launch({ headless: true });

try {
  const rendered = [];
  for (const sample of samples) {
    const page = await browser.newPage({ viewport: sample.viewport, deviceScaleFactor: 1 });
    await page.setContent(sample.render(), { waitUntil: "load" });
    await page.evaluate(() => document.fonts.ready);
    const filePath = join(outputDirectory, sample.file);
    await page.screenshot({ path: filePath, fullPage: false, animations: "disabled" });
    await page.close();
    rendered.push({
      sample_id: sample.sampleId,
      file: sample.file,
      sha256: await sha256File(filePath),
      declared_mime: "image/png",
      document_type: sample.documentType,
      scenario_tags: sample.scenarioTags,
      expected_fields: sample.expectedFields,
      expected_items: sample.expectedItems ?? [],
      expected_evidence: sample.expectedEvidence,
    });
  }

  const manifest = {
    preview_version: "chinese-business-v1",
    status: "preview_only_not_frozen",
    synthetic_only: true,
    eligible_for_tuning: false,
    eligible_for_release_evidence: false,
    generation_method: "deterministic_html_css_playwright",
    samples: rendered,
  };
  await writeFile(join(outputDirectory, "manifest.preview.json"), `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 });
} finally {
  await browser.close();
}

console.log(`rendered ${samples.length} Chinese business preview samples in ${outputDirectory}`);
