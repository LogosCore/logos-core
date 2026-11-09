import type { CSSProperties, ReactNode } from "react"
import { BlocksIcon, RadioTowerIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { DEFAULT_FACTORY_ICON, defaultIconFor, iconMaskImage, type ModuleIconSvgs } from "./module-icon-values"

const DEFAULT_ICONS = { channel: RadioTowerIcon, module: BlocksIcon }

/**
 * The lucide defaults' line, in the 24-unit grid: the modules' own icons'
 * (the HTTP, TCP and SSH channels draw at 0.85), not lucide's 2, so a module
 * without an icon sits beside the others at the same weight.
 */
const DEFAULT_STROKE = 0.85

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
    return (
      <IconPair
        image={iconMaskImage(icon.icon)}
        lightImage={icon.iconLight ? iconMaskImage(icon.iconLight) : null}
        className={className}
      />
    )
  }
  const kind = defaultIconFor(type)
  if (kind === "factory") {
    // Files of the app's own (public/module-icons), drawn as a module's pair.
    return (
      <IconPair
        image={`url("${DEFAULT_FACTORY_ICON.icon}")`}
        lightImage={`url("${DEFAULT_FACTORY_ICON.iconLight}")`}
        className={className}
      />
    )
  }
  const Icon = DEFAULT_ICONS[kind]
  return <Icon aria-hidden strokeWidth={DEFAULT_STROKE} className={cn("size-4 shrink-0", className)} />
}

/**
 * An icon and, when it has one, the one drawn instead on light themes: both
 * drawn, one hidden, as the theme is a class on the page and CSS picks.
 */
function IconPair({ image, lightImage, className }: { image: string; lightImage: string | null; className?: string }) {
  if (!lightImage) return <IconGlyph image={image} className={className} />
  return (
    <>
      <IconGlyph image={lightImage} className={cn(className, "dark:hidden")} />
      <IconGlyph image={image} className={cn(className, "hidden dark:inline-block")} />
    </>
  )
}

/** One image (a CSS url()), drawn through a mask in the text colour. */
function IconGlyph({ image, className }: { image: string; className?: string }) {
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
  return (
    <span aria-hidden data-module-icon className={cn("inline-block size-4 shrink-0 bg-current", className)} style={style} />
  )
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
