import { ServerIcon } from "lucide-react"
import { siAndroid, siApple, siFreebsd, siJavascript, siNetbsd, siOpenbsd, siWebassembly } from "simple-icons"
import { BrandSvgIcon, LinuxIcon, WindowsIcon } from "@/components/findings/host-icon"
import { cn } from "@/lib/utils"

// A session's OS as its hello says it (Go's GOOS names: the minions' and
// the build schema's), as an icon. Tux and the Windows panes are the host
// icons' (Simple Icons dropped Windows); the rest Simple Icons' marks. An OS
// with no mark of its own (dragonfly, solaris, illumos, aix, plan9, one this
// app does not know) is a plain server.

const MARKS: Record<string, string> = {
  darwin: siApple.path,
  ios: siApple.path,
  android: siAndroid.path,
  freebsd: siFreebsd.path,
  openbsd: siOpenbsd.path,
  netbsd: siNetbsd.path,
  js: siJavascript.path,
  wasip1: siWebassembly.path,
  wasm: siWebassembly.path,
}

/** The OS's icon, in the text's color. */
export function OsIcon({ os, size = 14, className }: { os: string | null | undefined; size?: number; className?: string }) {
  const name = (os ?? "").toLowerCase()
  if (name === "linux") return <LinuxIcon size={size} className={className} />
  if (name === "windows") return <WindowsIcon size={size} className={className} />
  const d = MARKS[name]
  if (d) return <BrandSvgIcon size={size} className={className} d={d} />
  return <ServerIcon className={cn("shrink-0", className)} style={{ width: size, height: size }} aria-hidden />
}
