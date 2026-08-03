const decodeFilename = (value: string) => {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

export const getHeaderString = (value: unknown): string | undefined =>
  typeof value === 'string' ? value : undefined

export const getDownloadFilename = (disposition?: string): string => {
  if (!disposition) return ''

  const encoded = disposition.match(/filename\*\s*=\s*UTF-8''([^;]+)/i)?.[1]
  if (encoded) return decodeFilename(encoded.trim().replace(/^"|"$/g, ''))

  const plain = disposition.match(/filename\s*=\s*(?:"([^"]+)"|([^;]+))/i)
  return (plain?.[1] || plain?.[2] || '').trim()
}

export const sanitizeDownloadFilename = (
  filename: string,
  fallback: string,
): string => {
  const sanitized = Array.from(filename.trim(), (character) => {
    const codePoint = character.codePointAt(0) ?? 0
    const reserved = '\\/:*?"<>|'.includes(character)
    return reserved || codePoint <= 31 || codePoint === 127 ? '_' : character
  })
    .join('')
    .replace(/\s+/g, ' ')
    .slice(0, 120)
  return sanitized || fallback
}
