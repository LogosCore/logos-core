// Status indicator follows the same dot-and-text idiom as the Users table's
// active/inactive cell. Registered is healthy/green, deregistered is a neutral
// "gone on purpose" amber, dead is a failure red.
const STATUS_STYLES: Record<string, { dot: string; text: string }> = {
  registered: {
    dot: "bg-green-600 dark:bg-green-400",
    text: "text-green-600 dark:text-green-400",
  },
  deregistered: {
    dot: "bg-amber-600 dark:bg-amber-400",
    text: "text-amber-600 dark:text-amber-400",
  },
  dead: {
    dot: "bg-red-600 dark:bg-red-400",
    text: "text-red-600 dark:text-red-400",
  },
}

export function ModuleStatusIndicator({ status }: { status: string }) {
  const s = STATUS_STYLES[status] ?? STATUS_STYLES.dead
  return (
    <span className={`inline-flex items-center gap-1.5 text-sm ${s.text}`}>
      <span className={`size-2 rounded-full ${s.dot}`} />
      {status}
    </span>
  )
}
