// Raw per-icon SVGs from the installed simple-icons package. Lazy (no
// { eager }), so each match is just a thunk until the icon first renders.
// simple-icons ships `icons/<slug>.svg`; the slug is the filename.
//
// Its own module so the HTML bundle's viewer can swap it for one that loads
// every SVG from a single script on first use (src/viewer/viewer-simple-icon-glob.ts).
export const SVG_GLOB: Record<string, () => Promise<string>> = import.meta.glob<string>(
  "/node_modules/simple-icons/icons/*.svg",
  { query: "?raw", import: "default" },
)
