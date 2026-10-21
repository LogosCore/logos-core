// Entry point of assets/simple-icons.js: every Simple Icon's SVG, keyed by
// slug, for viewer-simple-icon-glob.ts to load the first time a brand icon
// renders.

const SVGS = import.meta.glob<string>("/node_modules/simple-icons/icons/*.svg", {
  query: "?raw",
  import: "default",
  eager: true,
})

window.LogosSimpleIcons = Object.fromEntries(
  Object.entries(SVGS).map(([path, svg]) => [path.slice(path.lastIndexOf("/") + 1).replace(/\.svg$/, ""), svg]),
)
