// 文档与代码的两项机械一致性检查。加它们的原因是这两类漂移都真实发生过：
// 契约版本在代码里从 2/4/3 走到 5/5/4，文档停在原处三个版本无人察觉；
// 聊天的四张表落库两个版本后数据模型文档里一个字都没有。
//
// 两项检查都不做语义判断，只比对字符串集合，因此没有需要人工裁量的误报。
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";

// 契约版本的唯一真相在这几个 Go 常量里。改契约必须同时改它们，
// 检查据此反推文档该写什么，而不是反过来。
const contractSources = [
  ["apps/api/internal/application/processing/worker.go", ["promptVersion", "extractionSchemaVersion"]],
  ["apps/api/internal/application/claimmapping/mapper.go", ["Version", "ExtractionSchemaVersion", "ClaimSchemaVersion"]],
  ["apps/api/internal/adapters/openaicompatible/provider_schema.go", ["providerSchemaVersion"]],
];

// 「当前契约」只允许在这一行声明。其他文档要提当前版本就引用它，不再各写一份。
const canonicalDocument = "docs/ai-pipeline.md";
const canonicalPrefix = "当前契约：";

// 这两个文档描述的是当前系统，出现非当前版本即为漂移。历史版本沿革只属于
// ai-pipeline.md 的沿革段落与 ADR，不在这里出现。
const currentStateDocuments = ["docs/architecture.md", "docs/data-model.md"];

const contractToken = /\b(bill-visible-text-cn|bill-visible-text-provider|bill-visible-text|claim-mapper|document-claim)\/(\d+)/g;

export function parseGoConstants(source, names) {
  const found = new Map();
  for (const name of names) {
    const match = source.match(new RegExp(`\\b${name}\\s*=\\s*\`([^\`]+)\`|\\b${name}\\s*=\\s*"([^"]+)"`));
    if (!match) throw new Error(`constant not found: ${name}`);
    const value = match[1] ?? match[2];
    const token = value.match(/\b[a-z-]+\/\d+/);
    if (!token) throw new Error(`constant carries no contract token: ${name}`);
    found.set(name, token[0]);
  }
  return found;
}

export async function codeContracts(root) {
  const tokens = new Set();
  for (const [path, names] of contractSources) {
    const source = await readFile(join(root, path), "utf8");
    for (const token of parseGoConstants(source, names).values()) tokens.add(token);
  }
  return tokens;
}

export function canonicalLine(document) {
  const line = document.split("\n").find(entry => entry.startsWith(canonicalPrefix));
  if (!line) throw new Error("canonical contract declaration not found");
  return new Set([...line.matchAll(contractToken)].map(match => match[0]));
}

export function staleTokens(document, current) {
  return [...new Set([...document.matchAll(contractToken)].map(match => match[0]))]
    .filter(token => !current.has(token))
    .sort();
}

// 表名到文档实体名：documents -> Document、payment_invoice_links -> PaymentInvoiceLink。
// 文档按实体名书写，直接比对表名会把 51 张表里的 39 张报成缺失。
export function entityName(table) {
  const words = table.split("_");
  const last = words.length - 1;
  if (words[last].endsWith("ies")) words[last] = `${words[last].slice(0, -3)}y`;
  else if (words[last].endsWith("ses")) words[last] = words[last].slice(0, -2);
  else if (words[last].endsWith("s")) words[last] = words[last].slice(0, -1);
  return words.map(word => word.charAt(0).toUpperCase() + word.slice(1)).join("");
}

export async function migrationTables(root) {
  const directory = join(root, "infra/migrations");
  const tables = new Set();
  for (const file of (await readdir(directory)).sort()) {
    if (!file.endsWith(".sql")) continue;
    const sql = await readFile(join(directory, file), "utf8");
    for (const match of sql.matchAll(/CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+)/g)) tables.add(match[1]);
  }
  return [...tables].sort();
}

export function undocumentedTables(tables, document) {
  return tables.filter(table => !document.includes(table) && !document.includes(entityName(table)));
}

export async function check(root) {
  const current = await codeContracts(root);
  const canonicalText = await readFile(join(root, canonicalDocument), "utf8");
  const declared = canonicalLine(canonicalText);
  const missing = [...current].filter(token => !declared.has(token)).sort();
  const extra = [...declared].filter(token => !current.has(token)).sort();

  const drifted = [];
  for (const path of currentStateDocuments) {
    const stale = staleTokens(await readFile(join(root, path), "utf8"), current);
    if (stale.length) drifted.push({ document: path, stale_tokens: stale });
  }

  const tables = await migrationTables(root);
  const undocumented = undocumentedTables(tables, await readFile(join(root, "docs/data-model.md"), "utf8"));

  return {
    report_kind: "doc-consistency",
    contract_versions: [...current].sort(),
    contract_declaration_missing: missing,
    contract_declaration_unknown: extra,
    current_state_documents_drifted: drifted,
    migration_table_count: tables.length,
    undocumented_tables: undocumented,
    passed: !missing.length && !extra.length && !drifted.length && !undocumented.length,
  };
}

if (process.argv[1]?.endsWith("check-doc-consistency.mjs")) {
  const report = await check(process.argv[2] ?? process.cwd());
  process.stdout.write(`${JSON.stringify(report)}\n`);
  if (!report.passed) process.exit(1);
}
