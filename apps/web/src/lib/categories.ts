import type { Category } from '../api/types'

export function categoryLabelMap(categories: Category[]): Record<string, string> {
  return Object.fromEntries(categories.map(c => [c.Slug, c.Label]))
}

export function categoriesByType(categories: Category[], type: 'income' | 'expense'): Category[] {
  return categories.filter(c => c.Type === type)
}
