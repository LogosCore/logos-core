import fs from "fs"
import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"

// lucide-react@1.6.0 ships a broken dist tree:
//   - direct imports use `.ts` extensions instead of `.js`
//   - aliased icons re-export from `../<name>.ts` even though the target
//     actually lives in the same folder (`./<name>.js`)
// This resolver fixes both for any import originating inside
// lucide-react/dist/esm. Removing this plugin breaks the build.
function lucideFixTsExtension(): Plugin {
  return {
    name: "lucide-fix-ts-extension",
    enforce: "pre",
    async resolveId(id, importer) {
      if (
        !importer ||
        !importer.includes("/lucide-react/dist/esm/") ||
        !id.endsWith(".ts")
      ) {
        return null
      }
      // First: swap the bogus `.ts` extension for `.js`.
      const candidates = [id.replace(/\.ts$/, ".js")]
      // Second: aliased icons re-export from `../<name>.ts` but the target
      // is in the same directory; try the same-dir variant as a fallback.
      if (id.startsWith("../")) {
        candidates.push("./" + id.slice(3).replace(/\.ts$/, ".js"))
      }
      for (const candidate of candidates) {
        const resolved = await this.resolve(candidate, importer, {
          skipSelf: true,
        })
        if (resolved) return resolved
      }
      return null
    },
  }
}

// Excalidraw loads its hand-drawn fonts at runtime rather than bundling them.
// Given no window.EXCALIDRAW_ASSET_PATH it resolves them against its own CDN,
// and when that path is set it still keeps the CDN as a *fallback* candidate —
// so a wrong path degrades into a silent internet dependency rather than a
// visible error. This platform is deployed where there is no internet, so the
// fonts are served from our own origin instead.
//
// Served out of node_modules rather than committed to public/: the font tree
// is ~13 MB (Xiaolai, the CJK family, is all but 0.5 MB of it) and none of it
// belongs in git. Nothing here enters the JS bundle — these are static files
// fetched on demand, and only for a font actually used on a canvas.
//
// See EXCALIDRAW_ASSET_PATH in main.tsx, which must agree with the base below.
const EXCALIDRAW_ASSET_BASE = "/excalidraw-assets/"

function excalidrawAssets(): Plugin {
  const fontsDir = path.resolve(
    __dirname,
    "node_modules/@excalidraw/excalidraw/dist/prod/fonts",
  )

  return {
    name: "excalidraw-assets",

    // Dev server: map the same public path onto the package directory, so
    // development and production resolve fonts identically.
    configureServer(server) {
      server.middlewares.use(EXCALIDRAW_ASSET_BASE, (req, res, next) => {
        const rel = decodeURIComponent((req.url ?? "").split("?")[0])
        // Serve only from inside the font tree — reject any traversal.
        const target = path.resolve(fontsDir, "." + rel.replace(/^\/fonts/, ""))
        if (!target.startsWith(fontsDir) || !fs.existsSync(target)) {
          next()
          return
        }
        res.setHeader("Content-Type", "font/woff2")
        fs.createReadStream(target).pipe(res)
      })
    },

    // Build: copy the tree into the output so the deployed origin serves it.
    async writeBundle(options) {
      const outDir = options.dir ?? path.resolve(__dirname, "dist")
      const dest = path.join(outDir, EXCALIDRAW_ASSET_BASE.replace(/^\//, ""), "fonts")
      await fs.promises.cp(fontsDir, dest, { recursive: true })
    },
  }
}

// Preloads the one font the first screen is guaranteed to render in.
//
// Fonts are @import-ed by index.css, so the browser cannot know they exist
// until it has downloaded and parsed 163 KB of CSS — and only then starts
// fetching. That serialises two round trips in front of the first readable text,
// which on a slow machine is long enough to show the fallback face and then
// reflow to Geist.
//
// Only the latin sans subset is preloaded. @fontsource splits Geist by
// unicode-range (latin, latin-ext, cyrillic, vietnamese, symbols), and the
// browser fetches only the ranges a page actually uses, so preloading the rest
// would download bytes most sessions never need. Geist Mono is left out for the
// same reason: it renders code blocks, CIDRs and hashes, not the shell.
//
// `crossorigin` is required even same-origin — fonts are fetched in CORS mode,
// and a preload whose mode does not match is downloaded twice.
function preloadLatinFont(): Plugin {
  return {
    name: "preload-latin-font",
    apply: "build",
    transformIndexHtml: {
      order: "post",
      handler(html, ctx) {
        const font = Object.keys(ctx.bundle ?? {}).find((name) =>
          /geist-latin-wght-normal-[^/]*\.woff2$/.test(name),
        )
        // Silently skipped rather than failed: a font rename upstream should
        // cost the preload, not the build. The @import still works.
        if (!font) {
          this.warn("latin Geist woff2 not found in the bundle; no preload")
          return html
        }
        return {
          html,
          tags: [
            {
              tag: "link",
              attrs: {
                rel: "preload",
                href: `/${font}`,
                as: "font",
                type: "font/woff2",
                crossorigin: "",
              },
              injectTo: "head-prepend",
            },
          ],
        }
      },
    },
  }
}

// Files nginx is willing to serve pre-encoded, and the floor below which it is
// not worth it. The 1 KiB floor matches gzip_min_length in nginx.conf: under it
// the header and round-trip dominate and a compressed copy can even be larger.
// It also keeps ~4,000 per-icon chunks out of the image — see nginx.conf.
const PRECOMPRESS_EXTENSIONS = new Set([".js", ".css", ".html", ".svg", ".json"])
const PRECOMPRESS_MIN_BYTES = 1024

// Writes a .gz next to every compressible build output, for `gzip_static on`.
//
// nginx compresses on the fly at gzip_comp_level 1 today — its default, and a
// deliberately cheap one, because it pays that cost again for every request by
// every client. Compressing once at level 9 during the build takes the critical
// path from 305 KB to 269 KB and removes the per-request CPU entirely.
//
// Brotli is off by default rather than absent. At quality 11 it would take the
// same critical path to 230 KB (25% under today), but two things argue against
// shipping it on: the official nginx:1.27-alpine image has no brotli module, and
// Alpine's `nginx-mod-http-brotli` is built against nginx 1.26 — a dynamic
// module is version-locked, so it will not load. Emitting .br files nothing can
// serve would add ~4.7 MB to an image that is hand-carried to an air-gapped
// site. Flip this on together with a brotli-capable front end (see nginx.conf).
function precompressAssets({ brotli = false } = {}): Plugin {
  return {
    name: "precompress-assets",
    // Build only. closeBundle is not called by the dev server, so this is
    // belt-and-braces — but it also documents that nothing here should ever run
    // on a `vite dev` startup, where compressing 1,700 files would be pure lag.
    apply: "build",
    // After writeBundle, so files other plugins copy in (the Excalidraw font
    // tree) are on disk too — though none of those are compressible types.
    async closeBundle() {
      const zlib = await import("node:zlib")
      const outDir = path.resolve(__dirname, "dist")
      if (!fs.existsSync(outDir)) return

      const files: string[] = []
      const walk = (dir: string) => {
        for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
          const full = path.join(dir, entry.name)
          if (entry.isDirectory()) {
            walk(full)
          } else if (PRECOMPRESS_EXTENSIONS.has(path.extname(entry.name))) {
            files.push(full)
          }
        }
      }
      walk(outDir)

      let written = 0
      for (const file of files) {
        const source = await fs.promises.readFile(file)
        if (source.length < PRECOMPRESS_MIN_BYTES) continue

        const gz = zlib.gzipSync(source, { level: 9 })
        // A compressed copy that is not smaller would make nginx serve more
        // bytes than the original, so it is simply not written.
        if (gz.length < source.length) {
          await fs.promises.writeFile(`${file}.gz`, gz)
          written++
        }
        if (brotli) {
          const br = zlib.brotliCompressSync(source, {
            params: {
              [zlib.constants.BROTLI_PARAM_QUALITY]: 11,
              [zlib.constants.BROTLI_PARAM_SIZE_HINT]: source.length,
            },
          })
          if (br.length < source.length) {
            await fs.promises.writeFile(`${file}.br`, br)
            written++
          }
        }
      }
      this.info(`precompressed ${written} files`)
    },
  }
}

export default defineConfig({
  plugins: [
    lucideFixTsExtension(),
    excalidrawAssets(),
    // React Compiler on, via plugin-react's own `compiler` option (it drives
    // the Oxc-native `oxc-transform-react`, so there is no Babel pass in the
    // pipeline and no second parse of every file).
    //
    // It memoizes automatically, which this codebase had almost none of by
    // hand: 1 of 270 components used memo(), against 41 files with useMemo and
    // 18 with useCallback. On a weak CPU the cost of the UI is re-rendering
    // subtrees whose inputs did not change, and that is precisely what the
    // compiler removes — across every component, rather than wherever someone
    // remembered to reach for a hook.
    //
    // Anything the compiler cannot prove safe it skips and leaves exactly as
    // written, per component, so this is not all-or-nothing. eslint-plugin-
    // react-hooks v7 (already configured) reports those bail-outs as
    // `react-hooks/incompatible-library` and friends — `npx eslint .` is the
    // list of components still rendering unmemoized.
    react({ compiler: true }),
    tailwindcss(),
    preloadLatinFont(),
    precompressAssets(),
  ],
  build: {
    rollupOptions: {
      output: {
        codeSplitting: {
          groups: [
            // Once pages load lazily, the bundler cuts a module shared by
            // several chunks into a chunk of its own, and lucide icons are
            // shared twice over: between the app shell and the pages, and
            // with icon-catalog.ts's lazy imports, where lucide's alias files
            // (alert-circle is circle-alert) re-export the icon by its plain
            // path. That was a request of a few hundred bytes per icon, some
            // 150 on first paint. Keep the icons the entry loads anyway in
            // one chunk; the ones only pages use stay with those pages.
            {
              name: "icons",
              test: /lucide-react[\\/]dist[\\/]esm[\\/]icons[\\/]/,
              tags: ["$initial"],
            },
          ],
        },
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
  },
})
