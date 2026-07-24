import type { Entry } from '../../api/types'
import type { ExpenseComposition } from './types'

export function getExpenseComposition(
  entries: Entry[],
): ExpenseComposition[] {
  const byCategory = new Map<string, number>()

  for (const e of entries) {
    if (e.Type !== 'expense') continue
    const current = byCategory.get(e.Category) ?? 0
    byCategory.set(e.Category, current + e.Amount)
  }

  const total = Array.from(byCategory.values()).reduce((s, v) => s + v, 0)
  if (total === 0) return []

  return Array.from(byCategory.entries())
    .map(([categoryId, amount]) => ({
      categoryId,
      categoryName: formatCategoryName(categoryId),
      amount,
      percentage: Math.round((amount / total) * 100),
    }))
    .sort((a, b) => b.amount - a.amount)
}

function formatCategoryName(slug: string): string {
  return slug
    .replace(/_/g, ' ')
    .replace(/\b\w/g, c => c.toUpperCase())
}
