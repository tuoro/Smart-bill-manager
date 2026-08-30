/*
 * Smart Bill Manager M0 四页视觉基线复核脚本。
 *
 * 使用方式：通过本目录的本地 HTTP 服务打开 m0-d02-four-page-baseline.html；
 * 评审包装页会在 iframe 完成加载后自动加载本脚本。脚本只读取可信、纯合成的
 * 同源 srcdoc 评审内容，不依赖外部网络，也不得进入 M1 生产构建。
 * 视口测量前，把浏览器 CSS 视口设为“目标产品宽度 + 32 px”；包装层左右各
 * 16 px 留白，因此 iframe 的 innerWidth 即目标产品宽度。
 */
(() => {
  "use strict";

  const frame = document.querySelector("iframe");
  const doc = frame?.contentDocument;
  const root = doc?.querySelector("#sbm02-four-pages");
  if (!frame || !doc || !root) {
    throw new Error("未找到 M0 基线 iframe 或 #sbm02-four-pages");
  }
  const styleOf = doc.defaultView.getComputedStyle.bind(doc.defaultView);

  const stateButtons = () => [
    ...root.querySelectorAll("[data-scenario-page][data-scenario-state]"),
  ];

  const resetScenarios = () => {
    root
      .querySelectorAll('input[name="sbm02-link-choice"]')
      .forEach((radio) => {
        radio.checked = false;
      });
    for (const page of ["login", "inbox", "review", "bills"]) {
      root
        .querySelector(
          `[data-scenario-page="${page}"][data-scenario-state="default"]`,
        )
        ?.click();
    }
  };

  const currentTheme = () => root.dataset.previewTheme || "auto";

  const setTheme = (expected) => {
    const button = root.querySelector("[data-theme-cycle]");
    for (let attempt = 0; attempt < 4; attempt += 1) {
      if (currentTheme() === expected) return;
      button?.click();
    }
    throw new Error(`无法切换到主题：${expected}`);
  };

  const staticAudit = () => {
    const ids = [...doc.querySelectorAll("[id]")].map((node) => node.id);
    const duplicateIds = [
      ...new Set(ids.filter((id, index) => ids.indexOf(id) !== index)),
    ];
    const brokenRefs = [];
    for (const element of doc.querySelectorAll(
      "[aria-labelledby],[aria-describedby],[aria-controls],[for]",
    )) {
      for (const attribute of [
        "aria-labelledby",
        "aria-describedby",
        "aria-controls",
        "for",
      ]) {
        const value = element.getAttribute(attribute);
        if (!value) continue;
        for (const id of value.split(/\s+/)) {
          if (!doc.getElementById(id)) {
            brokenRefs.push({ attribute, id, tag: element.tagName });
          }
        }
      }
    }

    const externalResources = [document, doc].flatMap((scope) =>
      [...scope.querySelectorAll("[src],[href]")]
        .map((node) => node.getAttribute("src") || node.getAttribute("href"))
        .filter((value) => /^(https?:)?\/\//.test(value || "")),
    );
    const sections = [...root.querySelectorAll("main > section")].map(
      (section) => {
        const labelledBy = section.getAttribute("aria-labelledby") || "";
        const name =
          section.getAttribute("aria-label") ||
          labelledBy
            .split(/\s+/)
            .map((id) => doc.getElementById(id)?.textContent.trim() || "")
            .join(" ")
            .trim();
        return {
          name,
          window: section.querySelector("[data-page-window]")?.dataset
            .pageWindow,
        };
      },
    );
    const navLabels = [...doc.querySelectorAll("nav")]
      .map((node) => node.getAttribute("aria-label"))
      .filter(Boolean);

    const duplicateNavLabels = [
      ...new Set(
        navLabels.filter((label, index) => navLabels.indexOf(label) !== index),
      ),
    ];

    return {
      pass:
        doc.documentElement.lang === "zh-CN" &&
        doc.querySelectorAll("main").length === 1 &&
        sections.length === 4 &&
        sections.every((section) => section.name) &&
        duplicateIds.length === 0 &&
        brokenRefs.length === 0 &&
        duplicateNavLabels.length === 0 &&
        externalResources.length === 0 &&
        doc.querySelectorAll("i[data-lucide]").length === 0,
      lang: doc.documentElement.lang,
      title: doc.title,
      mainCount: doc.querySelectorAll("main").length,
      sections,
      duplicateIds,
      brokenRefs,
      navLabels,
      duplicateNavLabels,
      externalResources,
      svgCount: doc.querySelectorAll("svg").length,
      unhydratedIconCount: doc.querySelectorAll("i[data-lucide]").length,
      formCount: doc.querySelectorAll("form").length,
      headingCount: doc.querySelectorAll("h1,h2,h3,h4,h5,h6").length,
      tableCount: doc.querySelectorAll("table").length,
    };
  };

  const measureViewport = () => {
    const productWidth = doc.defaultView.innerWidth;
    const windows = [...root.querySelectorAll(".sbm02-window")].map(
      (windowNode) => ({
        page: windowNode.dataset.pageWindow,
        clientWidth: windowNode.clientWidth,
        scrollWidth: windowNode.scrollWidth,
        overflow: Math.max(0, windowNode.scrollWidth - windowNode.clientWidth),
      }),
    );
    const sidebar = root.querySelector(".sbm02-sidebar");
    const reviewGrid = root.querySelector(
      '[data-page-window="review"] .sbm02-review-grid',
    );
    const main = root.querySelector('[data-page-window="inbox"] .sbm02-main');
    const controls = [
      ["login", "[data-login-submit]"],
      ["upload", "[data-inbox-upload]"],
      ["confirm", "[data-review-confirm]"],
      ["bill", '[data-bill-tab="all"]'],
    ].map(([name, selector]) => {
      const node = root.querySelector(selector);
      const pageWindow = node.closest(".sbm02-window");
      const rect = node.getBoundingClientRect();
      const windowRect = pageWindow.getBoundingClientRect();
      return {
        name,
        width: Number(rect.width.toFixed(1)),
        height: Number(rect.height.toFixed(1)),
        inside:
          rect.left >= windowRect.left - 0.5 &&
          rect.right <= windowRect.right + 0.5 &&
          rect.top >= windowRect.top - 0.5 &&
          rect.bottom <= windowRect.bottom + 0.5,
        disabled: node.disabled,
      };
    });

    return {
      productWidth,
      rootClientWidth: root.clientWidth,
      rootScrollWidth: root.scrollWidth,
      rootOverflow: Math.max(0, root.scrollWidth - root.clientWidth),
      windows,
      sidebarWidth: Number(sidebar.getBoundingClientRect().width.toFixed(1)),
      reviewColumns: styleOf(reviewGrid).gridTemplateColumns
        .split(" ")
        .filter(Boolean).length,
      contentPadding: Number.parseFloat(styleOf(main).paddingLeft),
      controls,
      pass:
        root.scrollWidth === root.clientWidth &&
        windows.every((windowResult) => windowResult.overflow === 0) &&
        controls.every((control) => control.inside),
    };
  };

  const stateAudit = async () => {
    const rows = [];
    for (const button of stateButtons()) {
      button.click();
      await Promise.resolve();
      const page = button.dataset.scenarioPage;
      const state = button.dataset.scenarioState;
      const windowNode = root.querySelector(`[data-page-window="${page}"]`);
      rows.push({
        page,
        state,
        pressed: button.getAttribute("aria-pressed"),
        windowState: windowNode?.dataset.state,
        pass:
          button.getAttribute("aria-pressed") === "true" &&
          windowNode?.dataset.state === state,
      });
    }
    resetScenarios();
    return {
      count: rows.length,
      pass: rows.length === 19 && rows.every((row) => row.pass),
      failures: rows.filter((row) => !row.pass),
      rows,
    };
  };

  const rgb = (value) => {
    const parts = value.match(/[\d.]+/g)?.map(Number) || [];
    return {
      r: parts[0] || 0,
      g: parts[1] || 0,
      b: parts[2] || 0,
      a: parts.length > 3 ? parts[3] : 1,
    };
  };

  const luminance = (color) => {
    const channel = (value) => {
      const normalized = value / 255;
      return normalized <= 0.03928
        ? normalized / 12.92
        : ((normalized + 0.055) / 1.055) ** 2.4;
    };
    return (
      0.2126 * channel(color.r) +
      0.7152 * channel(color.g) +
      0.0722 * channel(color.b)
    );
  };

  const contrastRatio = (foreground, background) => {
    const first = luminance(foreground);
    const second = luminance(background);
    return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
  };

  const opaqueBackground = (node) => {
    for (let current = node; current; current = current.parentElement) {
      const color = rgb(styleOf(current).backgroundColor);
      if (color.a > 0.95) return color;
      if (current === root) break;
    }
    return { r: 255, g: 255, b: 255, a: 1 };
  };

  const contrastSnapshot = () => {
    const rows = [];
    for (const node of root.querySelectorAll(".sbm02-window *")) {
      const style = styleOf(node);
      if (
        style.display === "none" ||
        style.visibility === "hidden" ||
        Number(style.opacity) === 0
      ) {
        continue;
      }
      const text = [...node.childNodes]
        .filter((child) => child.nodeType === 3)
        .map((child) => child.textContent.trim())
        .join(" ")
        .trim();
      const rect = node.getBoundingClientRect();
      if (!text || !rect.width || !rect.height) continue;
      const ratio = contrastRatio(rgb(style.color), opaqueBackground(node));
      const fontSize = Number.parseFloat(style.fontSize);
      const fontWeight = Number.parseInt(style.fontWeight, 10) || 400;
      const large = fontSize >= 24 || (fontSize >= 18.66 && fontWeight >= 700);
      rows.push({
        text: text.slice(0, 64),
        fontSize,
        fontWeight,
        ratio: Number(ratio.toFixed(2)),
        pass: ratio >= (large ? 3 : 4.5),
      });
    }
    return {
      minimum: Number(Math.min(...rows.map((row) => row.ratio)).toFixed(2)),
      total: rows.length,
      failures: rows.filter((row) => !row.pass),
    };
  };

  const contrastAudit = async () => {
    const originalTheme = currentTheme();
    const rows = [];
    for (const theme of ["light", "dark"]) {
      setTheme(theme);
      for (const button of stateButtons()) {
        button.click();
        await Promise.resolve();
        const snapshot = contrastSnapshot();
        rows.push({
          theme,
          page: button.dataset.scenarioPage,
          state: button.dataset.scenarioState,
          minimum: snapshot.minimum,
          total: snapshot.total,
          failures: snapshot.failures,
        });
      }
    }
    resetScenarios();
    setTheme(originalTheme);
    return {
      count: rows.length,
      minimumLight: Math.min(
        ...rows.filter((row) => row.theme === "light").map((row) => row.minimum),
      ),
      minimumDark: Math.min(
        ...rows.filter((row) => row.theme === "dark").map((row) => row.minimum),
      ),
      visibleNodeRange: [
        Math.min(...rows.map((row) => row.total)),
        Math.max(...rows.map((row) => row.total)),
      ],
      pass: rows.length === 38 && rows.every((row) => !row.failures.length),
      failures: rows.filter((row) => row.failures.length),
      rows,
    };
  };

  const interactionAudit = async () => {
    const confirm = root.querySelector("[data-review-confirm]");
    const initialConfirmDisabled = confirm.disabled;

    root.querySelector('[data-inbox-filter="failed"]')?.click();
    const visibleInboxStatuses = [
      ...root.querySelectorAll("[data-inbox-row-status]"),
    ]
      .filter((row) => !row.hidden)
      .map((row) => row.dataset.inboxRowStatus);

    const evidence = [];
    for (const field of root.querySelectorAll("[data-review-field]")) {
      field.click();
      const key = field.dataset.reviewField;
      const target = root.querySelector(`[data-review-evidence="${key}"]`);
      evidence.push({
        key,
        pass:
          field.getAttribute("aria-pressed") === "true" &&
          target?.classList.contains("sbm02-paper-evidence-active"),
      });
    }

    const accept = root.querySelector(
      'input[name="sbm02-link-choice"][value="accept"]',
    );
    const reject = root.querySelector(
      'input[name="sbm02-link-choice"][value="reject"]',
    );
    accept.click();
    const enabledAfterAccept = !confirm.disabled;
    reject.click();
    confirm.click();
    const completed =
      root.querySelector('[data-page-window="review"]')?.dataset.state ===
        "completed" &&
      root.querySelector("[data-review-summary]")?.textContent.includes("已拒绝");

    root.querySelector('[data-bill-tab="payment"]')?.click();
    root.querySelector('[data-select-bill="payment-42"]')?.click();
    const selectedBill = [
      ...root.querySelectorAll("[data-bill-id]"),
    ].find((row) => row.dataset.selected === "true")?.dataset.billId;

    root.querySelector("[data-login-submit]")?.click();
    const loginLoading =
      root.querySelector('[data-page-window="login"]')?.dataset.state ===
      "loading";
    await new Promise((resolve) => setTimeout(resolve, 750));
    const loginError =
      root.querySelector('[data-page-window="login"]')?.dataset.state ===
        "error" &&
      root.querySelector("[data-login-email]")?.getAttribute("aria-invalid") ===
        "true";

    const result = {
      initialConfirmDisabled,
      visibleInboxStatuses,
      evidenceCount: evidence.length,
      evidenceFailures: evidence.filter((row) => !row.pass),
      enabledAfterAccept,
      completed,
      selectedBill,
      loginLoading,
      loginError,
    };
    result.pass =
      initialConfirmDisabled &&
      visibleInboxStatuses.join(",") === "failed" &&
      evidence.length === 7 &&
      !result.evidenceFailures.length &&
      enabledAfterAccept &&
      completed &&
      selectedBill === "payment-42" &&
      loginLoading &&
      loginError;

    root.querySelector('[data-inbox-filter="all"]')?.click();
    root.querySelector('[data-bill-tab="all"]')?.click();
    root.querySelector('[data-select-bill="invoice-18"]')?.click();
    resetScenarios();
    root.querySelector('[data-review-field="amount"]')?.click();
    return result;
  };

  const runAll = async () => ({
    static: staticAudit(),
    viewport: measureViewport(),
    states: await stateAudit(),
    contrast: await contrastAudit(),
  });

  globalThis.m0BaselineCheck = Object.freeze({
    staticAudit,
    measureViewport,
    stateAudit,
    contrastAudit,
    interactionAudit,
    runAll,
  });

  const publishResult = (result) => {
    let output = document.querySelector("#m0-browser-check-result");
    if (!output) {
      output = document.createElement("pre");
      output.id = "m0-browser-check-result";
      output.hidden = true;
      document.body.append(output);
    }
    output.textContent = JSON.stringify(result);
    document.documentElement.dataset.m0BrowserCheck = result.pass
      ? "pass"
      : "fail";
  };

  if (new URLSearchParams(location.search).get("audit") === "1") {
    document.documentElement.dataset.m0BrowserCheck = "running";
    runAll()
      .then(async (checks) => {
        const interactions = await interactionAudit();
        const result = {
          ...checks,
          interactions,
          pass:
            checks.static.pass &&
            checks.viewport.pass &&
            checks.states.pass &&
            checks.contrast.pass &&
            interactions.pass,
        };
        publishResult(result);
        console.info(`M0 自动复核完成：${result.pass ? "通过" : "失败"}`);
      })
      .catch((error) => {
        document.documentElement.dataset.m0BrowserCheck = "fail";
        console.error("M0 自动复核异常", error);
      });
  }

  console.info(
    "M0 复核脚本已安装：使用 ?audit=1 自动运行全部检查，或在 Console 调用 m0BaselineCheck。",
  );
})();
