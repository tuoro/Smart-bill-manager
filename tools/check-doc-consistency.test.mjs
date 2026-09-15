import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import {
  canonicalLine,
  check,
  composeMismatches,
  embeddedComposeBlocks,
  entityName,
  parseGoConstants,
  staleTokens,
  undocumentedTables,
} from "./check-doc-consistency.mjs";

const repositoryRoot = join(dirname(fileURLToPath(import.meta.url)), "..");

// 这条是门禁本身：文档里的当前契约版本、当前系统描述里的版本号，以及
// 每张迁移建的表，都必须与代码和数据模型文档对得上。
test("documentation agrees with the code it describes", async () => {
  const report = await check(repositoryRoot);
  assert.deepEqual(report.contract_declaration_missing, [], "当前契约声明缺少代码里在用的版本");
  assert.deepEqual(report.contract_declaration_unknown, [], "当前契约声明写了代码里不存在的版本");
  assert.deepEqual(report.current_state_documents_drifted, [], "描述当前系统的文档里留了旧契约版本");
  assert.deepEqual(report.undocumented_tables, [], "有迁移建的表没有写进 docs/data-model.md");
  assert.deepEqual(report.readme_compose_mismatches, [], "README 里贴的 compose 与仓库里的 compose.yaml 不一致");
  assert.ok(report.migration_table_count > 0);
  assert.equal(report.passed, true);
});

test("a code version bump that the declaration misses is reported both ways", () => {
  const current = new Set(["claim-mapper/6", "document-claim/4"]);
  const declared = canonicalLine("当前契约：`claim-mapper/5` / `document-claim/4`\n");
  assert.deepEqual([...current].filter(token => !declared.has(token)), ["claim-mapper/6"]);
  assert.deepEqual([...declared].filter(token => !current.has(token)), ["claim-mapper/5"]);
});

test("a stale version left in a current-state document is caught, a current one is not", () => {
  const current = new Set(["claim-mapper/5"]);
  assert.deepEqual(staleTokens("映射器 `claim-mapper/4` 负责绑定。", current), ["claim-mapper/4"]);
  assert.deepEqual(staleTokens("映射器 `claim-mapper/5` 负责绑定。", current), []);
});

test("missing declaration and unreadable constants fail loudly rather than pass empty", () => {
  assert.throws(() => canonicalLine("# 标题\n没有声明行。\n"), /canonical/);
  assert.throws(() => parseGoConstants('const other = "x"', ["promptVersion"]), /not found/);
  assert.throws(() => parseGoConstants('const promptVersion = "no-version-here"', ["promptVersion"]), /no contract token/);
});

test("table names map to the entity spelling the document actually uses", () => {
  assert.equal(entityName("documents"), "Document");
  assert.equal(entityName("payment_invoice_links"), "PaymentInvoiceLink");
  assert.equal(entityName("chat_identities"), "ChatIdentity");
  assert.equal(entityName("processing_jobs"), "ProcessingJob");
  // 未记录的表要报出来，按表名或实体名任一种写法记录都算数。
  assert.deepEqual(undocumentedTables(["widget_registrations"], "无关内容"), ["widget_registrations"]);
  assert.deepEqual(undocumentedTables(["widget_registrations"], "### WidgetRegistration"), []);
  assert.deepEqual(undocumentedTables(["widget_registrations"], "表 widget_registrations 见上"), []);
});

test("a README compose block that drifts from the shipped file is caught", () => {
  const shipped = "services:\n  app:\n    image: smart-bill-manager:v1\n";
  const same = `准备：\n\n\`\`\`yaml\n${shipped}\`\`\`\n`;
  assert.deepEqual(composeMismatches([["README.md", same]], shipped), []);

  const drifted = same.replace("v1", "v2");
  assert.deepEqual(composeMismatches([["README.md", drifted]], shipped), [
    { document: "README.md", reason: "compose block differs from compose.yaml" },
  ]);
});

test("a missing or duplicated compose block is reported rather than silently skipped", () => {
  const shipped = "services:\n  app:\n    image: smart-bill-manager:v1\n";
  const block = `\`\`\`yaml\n${shipped}\`\`\`\n`;
  assert.deepEqual(composeMismatches([["README.md", "没有部署文件。"]], shipped), [
    { document: "README.md", reason: "expected exactly one compose block, found 0" },
  ]);
  assert.deepEqual(composeMismatches([["README.md", block + block]], shipped), [
    { document: "README.md", reason: "expected exactly one compose block, found 2" },
  ]);
  // 只有真正的部署文件算数：讲解用的小片段不含服务定义，不参与比对。
  assert.deepEqual(embeddedComposeBlocks("```yaml\nx-database-password: &p 改我\n```\n"), []);
});
