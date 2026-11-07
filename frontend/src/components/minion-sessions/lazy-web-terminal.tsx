import { useLoaded } from "@/lib/preloadable"
import type { WebTerminalProps } from "./web-terminal"
import { webTerminalCode } from "./web-terminal-code"

/** The web terminal (web-terminal.tsx), shown once its code is loaded. */
export function LazyWebTerminal(props: WebTerminalProps) {
  const code = useLoaded(webTerminalCode)
  if (!code) {
    return <div className="flex flex-1 items-center justify-center rounded-md border text-sm text-muted-foreground">Loading the terminal…</div>
  }
  return <code.WebTerminal {...props} />
}
