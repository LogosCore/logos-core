import type { CSSProperties, ReactNode } from "react"
import { BlocksIcon, FactoryIcon, RadioTowerIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { defaultIconFor, iconMaskImage, type ModuleIconSvgs } from "./module-icon-values"

const DEFAULT_ICONS = { channel: RadioTowerIcon, factory: FactoryIcon, module: BlocksIcon }

/**
 * A module's icon: the SVG it sent at registration, drawn in the text colour,
 * or else its type's default. On light themes its light-theme SVG, when it
 * sent one (an icon drawn as the lit parts of a shaded picture is a negative
 * in a dark colour). Sized like the other icons (size-4) unless className
 * says otherwise.
 */
export function ModuleIcon({
  type,
  icon,
  className,
}: {
  type: string
  icon?: ModuleIconSvgs | null
  className?: string
}) {
  if (icon?.icon) {
    if (icon.iconLight) {
      // Both drawn, one hidden: the theme is a class on the page, so CSS picks.
      return (
        <>
          <IconGlyph svg={icon.iconLight} className={cn(className, "dark:hidden")} />
          <IconGlyph svg={icon.icon} className={cn(className, "hidden dark:inline-block")} />
        </>
      )
    }
    return <IconGlyph svg={icon.icon} className={className} />
  }
  const Icon = DEFAULT_ICONS[defaultIconFor(type)]
  return <Icon aria-hidden className={cn("size-4 shrink-0", className)} />
}

/** One SVG, drawn through a mask in the text colour. */
function IconGlyph({ svg, className }: { svg: string; className?: string }) {
  const image = iconMaskImage(svg)
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

/**
 * A channel or factory kind's name with its icon, where operators pick or see
 * one. In a line of text the name sits on the line's baseline, like the words
 * around it, and the icon is centred on the line.
 */
export function KindName({
  type,
  name,
  icon,
  className,
}: {
  type: "channel" | "factory"
  name: string
  icon?: ModuleIconSvgs | null
  className?: string
}) {
  return (
    <span className={cn("inline-flex min-w-0 items-baseline gap-2", className)}>
      <ModuleIcon type={type} icon={icon} className="size-5 self-center" />
      <span className="truncate font-mono">{name}</span>
    </span>
  )
}

/**
 * The lead cell of a table row about a module or what one serves (Modules,
 * Endpoints, Builds): the icon in the text colour, as tall as the two lines
 * beside it, a title line and a muted second line.
 */
export function ModuleIconCell({
  type,
  icon,
  title,
  subtitle,
  subtitleTitle,
}: {
  type: string
  icon?: ModuleIconSvgs | null
  title: ReactNode
  subtitle?: ReactNode
  /** The second line's full text on hover. */
  subtitleTitle?: string
}) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <ModuleIcon type={type} icon={icon} className="size-9" />
      <div className="flex min-w-0 flex-col">
        <div className="flex min-w-0 items-baseline">{title}</div>
        {subtitle && (
          <div className="truncate text-xs text-muted-foreground" title={subtitleTitle}>
            {subtitle}
          </div>
        )}
      </div>
    </div>
  )
}
