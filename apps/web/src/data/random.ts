// crypto.randomUUID 只在安全上下文（HTTPS 或 localhost）里存在。局域网明文访问
// （http://192.168.x.x）时它是 undefined，直接调用会抛 TypeError——请求还没发出去
// 就失败了，页面却按「结果未知」处理。getRandomValues 没有这个限制，用它拼一个
// RFC 4122 v4 UUID 做兜底。
export function randomUUID(): string {
  const cryptoObject = globalThis.crypto
  if (typeof cryptoObject?.randomUUID === 'function') return cryptoObject.randomUUID()
  const bytes = new Uint8Array(16)
  cryptoObject.getRandomValues(bytes)
  bytes[6] = (bytes[6]! & 0x0f) | 0x40
  bytes[8] = (bytes[8]! & 0x3f) | 0x80
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
