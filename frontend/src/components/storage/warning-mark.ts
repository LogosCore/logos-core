// The sidebar's Storage mark, apart from the Storage page's other helpers
// (storage-values.ts): the app shell imports this module, and a module the
// shell shares with a lazy page puts everything the page takes of it on
// first paint.

/** The sidebar's mark: the warnings that need an admin (errors and warnings, not tips), and the worst of them. */
export function warningMark(warnings: readonly { level: string }[]): { count: number; level: "error" | "warning" } | null {
  const needing = warnings.filter((w) => w.level === "error" || w.level === "warning")
  if (needing.length === 0) return null
  return { count: needing.length, level: needing.some((w) => w.level === "error") ? "error" : "warning" }
}
