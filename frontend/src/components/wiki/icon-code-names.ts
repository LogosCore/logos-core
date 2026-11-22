// Tooltips that say an icon's or an emoji's value as code writes it, for a
// picker whose choice someone may need to write by hand: a deployment's
// built-in process marks (the JSON file core reads at start; docs:
// core-infrastructure) take a mark's color, icon and emoji as strings, and
// these are those strings.

/** An icon's tooltip: icon: <its name as stored> ("Flame", "si:github"). */
export function iconCodeTitle(name: string): string {
  return `icon: ${name}`
}

/**
 * An emoji's tooltip: its name, then the emoji itself as stored and its code
 * points. Some emoji carry an invisible variation selector (⚙️ is U+2699
 * U+FE0F) or join several (👩‍💻), which a copy of the glyph can lose.
 */
export function emojiCodeTitle(name: string, native: string): string {
  const points = Array.from(native, (c) => `U+${(c.codePointAt(0) ?? 0).toString(16).toUpperCase().padStart(4, "0")}`)
  return `${name}\nemoji: ${native} (${points.join(" ")})`
}

/** A mark color's or style's tooltip: its label, then color: <its id>. */
export function markColorCodeTitle(label: string, id: string): string {
  return `${label}\ncolor: ${id}`
}

/** The part of emoji-mart's dataset (@emoji-mart/data) that names emoji. */
export interface EmojiNameData {
  emojis: Record<string, { name: string; skins: readonly { native: string }[] }>
}

/**
 * emoji-mart's name of each emoji, by the emoji itself (each skin tone's
 * too): the name its picker's own titles say, from the same data.
 */
export function emojiNames(data: EmojiNameData): Map<string, string> {
  const names = new Map<string, string>()
  for (const emoji of Object.values(data.emojis)) {
    for (const skin of emoji.skins) names.set(skin.native, emoji.name)
  }
  return names
}

/**
 * Titles every emoji emoji-mart has drawn under root with titleOf. An emoji
 * is a button with aria-posinset (a category's button has none), and its
 * aria-label is the emoji itself. Its name comes from names, not from its
 * title, which may be one this wrote; an emoji names lacks keeps
 * emoji-mart's title. A title already right is not written again, so a pass
 * over titled emoji writes nothing.
 */
export function retitleEmojis(
  root: ParentNode,
  names: ReadonlyMap<string, string>,
  titleOf: (name: string, native: string) => string,
): void {
  for (const button of root.querySelectorAll<HTMLElement>("button[aria-posinset]")) {
    const native = button.getAttribute("aria-label") ?? ""
    const name = names.get(native)
    if (name === undefined) continue
    const title = titleOf(name, native)
    if (button.title !== title) button.title = title
  }
}
