import type { CSSProperties } from "react"
import { BlocksIcon, FactoryIcon, RadioTowerIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { defaultIconFor, iconMaskImage } from "./module-icon-values"

const DEFAULT_ICONS = { channel: RadioTowerIcon, factory: FactoryIcon, module: BlocksIcon }

/**
 * A module's icon: the SVG it sent at registration, drawn in the text colour,
 * or else its type's default. Sized like the other icons (size-4) unless
 * className says otherwise.
 */
export function ModuleIcon({ type, icon, className }: { type: string; icon?: string | null; className?: string }) {
  if (icon) {
    const image = iconMaskImage(icon)
    const style: CSSProperties = {
      maskImage: image,
      WebkitMaskImage: image,
      maskSize: "contain",
      WebkitMaskSize: "contain",
      maskRepeat: "no-repeat",
      WebkitMaskRepeat: "no-repeat",
      maskPosition: "center",
      WebkitMaskPosition: "center",
    }
    return <span aria-hidden className={cn("inline-block size-4 shrink-0 bg-current", className)} style={style} />
  }
  const Icon = DEFAULT_ICONS[defaultIconFor(type)]
  return <Icon aria-hidden className={cn("size-4 shrink-0", className)} />
}
