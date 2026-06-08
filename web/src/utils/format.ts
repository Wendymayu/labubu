export function formatCost(cost: number | null | undefined, currency?: string): string {
  if (cost == null) return '-'
  const curr = currency || 'USD'
  const symbol = curr === 'CNY' ? '¥' : '$'
  if (cost < 0.01) {
    return `${symbol}${cost.toFixed(4)}`
  }
  return `${symbol}${cost.toFixed(2)}`
}
