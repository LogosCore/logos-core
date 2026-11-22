import data, { type EmojiMartData } from "@emoji-mart/data"
import { expect, test } from "vitest"
import { emojiCodeTitle, emojiNames, iconCodeTitle, markColorCodeTitle, retitleEmojis } from "./icon-code-names"

test("an icon's value as code writes it", () => {
  expect(iconCodeTitle("Flame")).toBe("icon: Flame")
  expect(iconCodeTitle("si:github")).toBe("icon: si:github")
})

test("an emoji's value, its code points with it", () => {
  expect(emojiCodeTitle("Fox", "🦊")).toBe("Fox\nemoji: 🦊 (U+1F98A)")
  // The invisible variation selector, and a joined sequence, said too.
  expect(emojiCodeTitle("Gear", "⚙️")).toBe("Gear\nemoji: ⚙️ (U+2699 U+FE0F)")
  expect(emojiCodeTitle("Woman Technologist", "👩‍💻")).toBe("Woman Technologist\nemoji: 👩‍💻 (U+1F469 U+200D U+1F4BB)")
})

test("a mark color's value", () => {
  expect(markColorCodeTitle("Red, strong", "red-strong")).toBe("Red, strong\ncolor: red-strong")
})

test("emoji-mart's names, by each emoji and its skin tones", () => {
  // The dataset's typings declare its interfaces, not the JSON it exports.
  const names = emojiNames(data as unknown as EmojiMartData)
  expect(["👍", "👍🏽", "⚙️", "🦊", "👩‍💻"].map((n) => names.get(n))).toEqual([
    "Thumbs Up",
    "Thumbs Up",
    "Gear",
    "Fox",
    "Woman Technologist",
  ])
})

// A stand-in for an emoji's button: its emoji, its title, and how often the
// title was written.
const emoji = (native: string, title: string) => {
  let current = title
  const button = {
    writes: 0,
    getAttribute: (k: string) => (k === "aria-label" ? native : null),
    get title() {
      return current
    },
    set title(t: string) {
      current = t
      button.writes++
    },
  }
  return button
}
// A stand-in for the picker's shadow root: the emoji drawn in it, by the
// selector an emoji's button answers.
const drawn = (buttons: object[]) =>
  ({ querySelectorAll: (selector: string) => (selector === "button[aria-posinset]" ? buttons : []) }) as unknown as ParentNode

test("every emoji drawn takes its tooltip, written once", () => {
  const names = new Map([
    ["🦊", "Fox"],
    ["👍", "Thumbs Up"],
  ])
  const titleOf = (name: string, native: string) => `${name} / ${native}`
  const fox = emoji("🦊", "Fox")
  const thumbs = emoji("👍", "Thumbs Up")
  const unknown = emoji("🐉", "Dragon")
  const root = drawn([fox, thumbs, unknown])
  retitleEmojis(root, names, titleOf)
  expect([fox.title, thumbs.title]).toEqual(["Fox / 🦊", "Thumbs Up / 👍"])
  // One the names lack keeps emoji-mart's title.
  expect(unknown.title).toBe("Dragon")
  // Again: the name read from the data, not from the title written, and
  // nothing written twice.
  retitleEmojis(root, names, titleOf)
  expect([fox.title, fox.writes, thumbs.writes, unknown.writes]).toEqual(["Fox / 🦊", 1, 1, 0])
})
