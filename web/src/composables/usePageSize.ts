// Per-list page-size preference persisted to localStorage under a scope-specific
// key, so each list (traces / sessions / logs) remembers its own size.
export const PAGE_SIZE_OPTIONS: readonly number[] = [10, 20, 50, 100]
const DEFAULT_PAGE_SIZE = 20

export function usePageSize(scope: string) {
  const storageKey = `labubu.${scope}.pageSize`

  function loadPageSize(): number {
    const v = Number(localStorage.getItem(storageKey))
    return PAGE_SIZE_OPTIONS.includes(v) ? v : DEFAULT_PAGE_SIZE
  }

  function savePageSize(n: number): void {
    if (PAGE_SIZE_OPTIONS.includes(n)) localStorage.setItem(storageKey, String(n))
  }

  return { options: PAGE_SIZE_OPTIONS, loadPageSize, savePageSize }
}
