export function formatCost(cost: number | null | undefined, currency?: string): string {
  if (cost == null) return '-'
  const curr = currency || 'USD'
  const symbol = curr === 'CNY' ? '¥' : '$'
  if (cost < 0.01) {
    return `${symbol}${cost.toFixed(4)}`
  }
  return `${symbol}${cost.toFixed(2)}`
}

export function formatNumber(n: number | null | undefined): string {
  if (n == null) return '-'
  if (n >= 1_000_000) {
    return `${(n / 1_000_000).toFixed(1)}M`
  }
  if (n >= 1_000) {
    return `${(n / 1_000).toFixed(1)}K`
  }
  return n.toString()
}

export function highlightJSON(raw: string): string {
  try {
    const parsed = JSON.parse(raw)
    const pretty = JSON.stringify(parsed, null, 2)
    return pretty
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"([^"]+)":/g, '<span class="j-key">"$1"</span>:')
      .replace(/: "([^"]*)"/g, ': <span class="j-str">"$1"</span>')
      .replace(/: (\d+\.?\d*)/g, ': <span class="j-num">$1</span>')
      .replace(/: (true|false|null)/g, ': <span class="j-bool">$1</span>')
  } catch {
    return raw
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
  }
}
