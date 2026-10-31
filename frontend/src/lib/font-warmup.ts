// Fonts a page may first need long after it loaded, fetched while it is idle
// instead. The first Cyrillic or full-width character a page shows made all
// of its text blink for a moment (an ls listing Ukrainian file names and
// "：", "｜" in others):
//
// - Geist and Geist Mono come in a file per script (@fontsource's
//   unicode-range split), and only latin Geist is preloaded (vite.config.ts).
//   The first Cyrillic fetched its file, and Chrome laid all text out again
//   when it came. Both Cyrillic files are about 28 KB together.
// - No web font covers full-width forms. Chrome looks for a system font that
//   does (on Linux usually a large CJK one) the first time one is drawn. It
//   is drawn here once, in a corner, too faint to see: the lookup only
//   happens when text is laid out and painted, not through document.fonts.

const CYRILLIC = "Жж"
const FULLWIDTH = "：｜"
const FAMILIES = ['"Geist Variable"', '"Geist Mono Variable"']

function warm() {
  for (const family of FAMILIES) {
    // A failed fetch only means the font comes when it is first needed.
    document.fonts.load(`400 16px ${family}`, CYRILLIC).catch(() => {})
  }
  const box = document.createElement("div")
  box.setAttribute("aria-hidden", "true")
  box.style.cssText =
    "position:fixed;left:0;top:0;width:1px;height:1px;overflow:hidden;opacity:0.01;pointer-events:none;z-index:2147483647;white-space:nowrap"
  for (const font of ["var(--font-sans)", "var(--font-mono)"]) {
    const span = document.createElement("span")
    span.style.fontFamily = font
    span.textContent = CYRILLIC + FULLWIDTH
    box.appendChild(span)
  }
  document.body.appendChild(box)
  // Long enough to be painted; the fonts stay loaded after.
  setTimeout(() => box.remove(), 5000)
}

/** Warms the fonts once the page is idle after its first paint. */
export function warmFontsWhenIdle() {
  if (typeof document === "undefined" || !document.fonts) return
  if ("requestIdleCallback" in window) window.requestIdleCallback(warm, { timeout: 3000 })
  else setTimeout(warm, 1500)
}
