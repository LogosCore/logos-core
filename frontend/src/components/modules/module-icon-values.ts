/**
 * A module icon's SVG as a CSS image. The UI draws it through a mask filled
 * with the text colour, so it matches the built-in icons in either theme, and
 * nothing in it runs: an image used as a mask loads nothing and runs no
 * script (core has also checked it at registration).
 */
export function iconMaskImage(svg: string): string {
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}")`
}

export type DefaultIcon = "channel" | "factory" | "module"

/** The built-in icon a module without its own shows, by module type. */
export function defaultIconFor(type: string): DefaultIcon {
  if (type === "channel" || type === "factory") return type
  return "module"
}
