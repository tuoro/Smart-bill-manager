// 评测工具只累计正式 Fact 数量，不将分页记录全部保存在内存中。
export async function countFactPages(client, collection) {
  if (!["/payments", "/invoices"].includes(collection)) {
    throw new Error("unsupported fact collection");
  }
  let count = 0;
  let cursor = "";
  const visited = new Set();
  do {
    const params = new URLSearchParams({ limit: "100" });
    if (cursor) params.set("cursor", cursor);
    const page = await client.get(`${collection}?${params}`);
    if (!Array.isArray(page.items) || page.items.length > 100 || typeof page.next_cursor !== "string") {
      throw new Error("invalid fact page");
    }
    count += page.items.length;
    if (!Number.isSafeInteger(count)) throw new Error("fact count overflow");
    cursor = page.next_cursor;
    if (cursor && (visited.has(cursor) || !page.items.length)) throw new Error("non-progressing fact page");
    visited.add(cursor);
  } while (cursor);
  return count;
}
