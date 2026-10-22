import fs from "fs"
import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"
import { lucideFixTsExtension } from "./vite.config"

// Builds the HTML bundle's offline viewer (src/viewer) into the Go package
// that embeds it: core/pkg/wikitransfer/htmlbundle/viewer. Two runs of this
// config, chosen by VIEWER_ENTRY, because each output is one classic script:
//
//   viewer        → assets/viewer.js + assets/viewer.css, and index.html
//   drawing       → assets/drawing.js (Excalidraw's SVG export, on demand)
//   simple-icons  → assets/simple-icons.js (brand icon SVGs, on demand)
//
// Why classic scripts in a single file each: the bundle is opened from disk,
// and browsers refuse ES modules and fetch() on file:// pages. So no
// type="module", no chunks loaded at runtime, and every asset — fonts
// included — inlined as a data URL, because a font file next to the page is
// a cross-origin request under file:// and is refused too.
//
// `npm run build:viewer` runs both. The app's own build (vite.config.ts) is
// untouched, and so is its bundle budget: none of this is on its first paint.

const ENTRIES = {
  viewer: "src/viewer/main.tsx",
  drawing: "src/viewer/drawing-entry.ts",
  "simple-icons": "src/viewer/simple-icons-entry.ts",
} as const
type Entry = keyof typeof ENTRIES
const entry: Entry = (process.env.VIEWER_ENTRY as Entry) in ENTRIES ? (process.env.VIEWER_ENTRY as Entry) : "viewer"
const outRoot = path.resolve(__dirname, "../core/pkg/wikitransfer/htmlbundle/viewer")

// index.html is static and sits beside assets/, not in it.
function copyIndexHtml(): Plugin {
  return {
    name: "viewer-index-html",
    apply: "build",
    closeBundle() {
      fs.copyFileSync(path.resolve(__dirname, "src/viewer/index.html"), path.join(outRoot, "index.html"))
    },
  }
}

// The slugs of every installed Simple Icon, for viewer-simple-icon-glob.ts:
// what the catalog needs synchronously, without the SVGs themselves.
function simpleIconSlugs(): Plugin {
  const id = "virtual:simple-icon-slugs"
  return {
    name: "viewer-simple-icon-slugs",
    resolveId: (source) => (source === id ? "\0" + id : null),
    load(loaded) {
      if (loaded !== "\0" + id) return null
      const dir = path.resolve(__dirname, "node_modules/simple-icons/icons")
      const slugs = fs
        .readdirSync(dir)
        .filter((f) => f.endsWith(".svg"))
        .map((f) => f.slice(0, -4))
        .sort()
      return `export const SIMPLE_ICON_SLUGS = ${JSON.stringify(slugs)}`
    },
  }
}

export default defineConfig({
  plugins: [
    lucideFixTsExtension(),
    react({ compiler: true }),
    tailwindcss(),
    simpleIconSlugs(),
    ...(entry === "viewer" ? [copyIndexHtml()] : []),
  ],
  resolve: {
    alias: [
      // One object of every lucide icon instead of one module each; see
      // src/viewer/viewer-icon-glob.ts. Listed before "@" so it wins.
      {
        find: /^@\/components\/wiki\/icon-glob$/,
        replacement: path.resolve(__dirname, "src/viewer/viewer-icon-glob.ts"),
      },
      // No server to ask; see src/viewer/viewer-file-type-probe.ts.
      {
        find: /^@\/components\/wiki\/wiki-file-type-probe$/,
        replacement: path.resolve(__dirname, "src/viewer/viewer-file-type-probe.ts"),
      },
      // TIFF and HEIC decoders; see src/viewer/viewer-image-decode.ts.
      {
        find: /^@\/components\/wiki\/wiki-image-decode$/,
        replacement: path.resolve(__dirname, "src/viewer/viewer-image-decode.ts"),
      },
      {
        find: /^@\/components\/wiki\/simple-icon-glob$/,
        replacement: path.resolve(__dirname, "src/viewer/viewer-simple-icon-glob.ts"),
      },
      { find: "@", replacement: path.resolve(__dirname, "./src") },
    ],
  },
  // Library mode does not replace this on its own, and React reads it.
  define: {
    "process.env.NODE_ENV": JSON.stringify("production"),
  },
  build: {
    outDir: path.join(outRoot, "assets"),
    // Both runs write into the same directory.
    emptyOutDir: false,
    copyPublicDir: false,
    assetsInlineLimit: () => true,
    cssCodeSplit: false,
    sourcemap: false,
    lib: {
      entry: path.resolve(__dirname, ENTRIES[entry]),
      formats: ["iife"],
      name: `LogosViewer_${entry.replace("-", "_")}`,
      fileName: () => `${entry}.js`,
      cssFileName: entry,
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
      },
    },
  },
})
