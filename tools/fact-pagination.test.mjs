import assert from "node:assert/strict";
import { test } from "node:test";
import { countFactPages } from "./fact-pagination.mjs";

test("count uses every bounded page, not the first-page length", async () => {
  const paths = [];
  const pages = [
    { items: Array.from({ length: 100 }, () => ({})), next_cursor: "next+one" },
    { items: Array.from({ length: 100 }, () => ({})), next_cursor: "next-two" },
    { items: [{}], next_cursor: "" },
  ];
  assert.equal(await countFactPages({ get: async path => { paths.push(path); return pages.shift(); } }, "/payments"), 201);
  assert.equal(paths[1], "/payments?limit=100&cursor=next%2Bone");
});
test("empty, malformed, non-progressing and failed queries remain explicit", async () => {
  assert.equal(await countFactPages({ get: async () => ({ items: [], next_cursor: "" }) }, "/invoices"), 0);
  for (const page of [{ items: [] }, { items: [], next_cursor: "next" }, { items: [{}], next_cursor: "cycle" }]) {
    await assert.rejects(countFactPages({ get: async () => page }, "/payments"));
  }
  await assert.rejects(countFactPages({ get: async () => { throw new Error("synthetic failure"); } }, "/payments"), /synthetic failure/);
  await assert.rejects(countFactPages({}, "/documents"));
});
