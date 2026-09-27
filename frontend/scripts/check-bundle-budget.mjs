// Fails the build when the first-paint critical path grows past its budget.
//
// The critical path is the entry script plus every modulepreload and stylesheet
// index.html names: exactly what a cold browser must download, parse and compile
// before React mounts. Anything reached by a lazy route is deliberately not
// counted — those load after first paint and are supposed to be large.
//
// This exists because the expensive regressions in this app are not written as
// performance mistakes. They are a static import added to a module the shell
// already reaches, which is invisible in review and moves a few hundred KB onto
// first paint. Three of them had accumulated: a Cmd-K palette pulling 1 MB of
// icon catalogs into the shell, a favicon hook putting 370 KB into every page's
// chunk, and graphql-js shipped to print query strings. A number that fails here
// is how the next one gets noticed on the pull request instead of in a profile.
//
// RAW is the budget that matters most. On a low-end machine the binding cost of
// a bundle is parse and compile, which scales with bytes of source, not with the
// compressed size on the wire.

import fs from "node:fs"
import path from "node:path"
import zlib from "node:zlib"

// Budgets are set just above what the tree costs today, so a regression that
// matters trips them and ordinary feature work does not. They are not aspirations
// — lowering them after a win is the point, and raising one should take an
// explanation in the commit message.
//
// At the time of writing the critical path is 964 KB raw / 269 KB served.
const BUDGET_RAW_KB = 1_050
const BUDGET_SERVED_KB = 300

const dist = path.resolve(import.meta.dirname, "..", "dist")
const indexHtml = path.join(dist, "index.html")

if (!fs.existsSync(indexHtml)) {
  console.error(
    `No build found at ${dist}. Run \`npm run build\` before this check.`,
  )
  process.exit(1)
}

const html = fs.readFileSync(indexHtml, "utf8")
const refs = [...html.matchAll(/(?:src|href)="\/assets\/([^"]+)"/g)].map(
  (m) => m[1],
)

if (refs.length === 0) {
  // A build that names no assets means the regex stopped matching what Vite
  // emits, not that the app got free. Failing loudly beats reporting 0 KB.
  console.error(
    "index.html referenced no /assets/ files — the build output shape changed.",
  )
  process.exit(1)
}

// Fonts are subset by unicode-range and parse as nothing; counting them in a
// budget meant to track JavaScript cost would make a font change look like a
// code regression. Reported, not budgeted.
const isFont = (f) => /\.(woff2?|ttf|otf|eot)$/.test(f)

const rows = []
for (const ref of refs) {
  const file = path.join(dist, "assets", ref)
  if (!fs.existsSync(file)) continue
  const source = fs.readFileSync(file)
  // Prefer the .gz the build wrote, since that is the byte count nginx serves
  // via gzip_static. Fall back to compressing here so the check still works on
  // a build made with precompression off.
  const gzPath = `${file}.gz`
  const served = fs.existsSync(gzPath)
    ? fs.statSync(gzPath).size
    : zlib.gzipSync(source, { level: 9 }).length
  rows.push({ ref, raw: source.length, served, font: isFont(ref) })
}

const code = rows.filter((r) => !r.font)
const fonts = rows.filter((r) => r.font)
const sum = (list, key) => list.reduce((a, r) => a + r[key], 0)

const rawKB = sum(code, "raw") / 1024
const servedKB = sum(code, "served") / 1024

const kb = (n) => `${(n / 1024).toFixed(1)} KB`.padStart(10)

console.log("First-paint critical path\n")
for (const r of [...code].sort((a, b) => b.raw - a.raw)) {
  console.log(`  ${kb(r.raw)} raw ${kb(r.served)} served  ${r.ref}`)
}
console.log(
  `\n  ${rawKB.toFixed(1)} KB raw / ${servedKB.toFixed(1)} KB served over ` +
    `${code.length} requests`,
)
if (fonts.length > 0) {
  console.log(
    `  plus ${(sum(fonts, "raw") / 1024).toFixed(1)} KB of preloaded fonts ` +
      `(not budgeted — see above)`,
  )
}

const failures = []
if (rawKB > BUDGET_RAW_KB) {
  failures.push(
    `raw ${rawKB.toFixed(1)} KB exceeds the ${BUDGET_RAW_KB} KB budget ` +
      `by ${(rawKB - BUDGET_RAW_KB).toFixed(1)} KB`,
  )
}
if (servedKB > BUDGET_SERVED_KB) {
  failures.push(
    `served ${servedKB.toFixed(1)} KB exceeds the ${BUDGET_SERVED_KB} KB ` +
      `budget by ${(servedKB - BUDGET_SERVED_KB).toFixed(1)} KB`,
  )
}

if (failures.length > 0) {
  console.error(`\nBundle budget exceeded:`)
  for (const f of failures) console.error(`  - ${f}`)
  console.error(
    `\nThe usual cause is a new static import in a module the app shell already
reaches, which pulls a lazy chunk onto first paint. Look for an import added to
app-layout.tsx, a hook every page calls, or a module that re-exports both a store
and the component that uses it — and make it a dynamic import or split the store
out. If the growth is genuinely required, raise the budget in this file and say
why in the commit message.`,
  )
  process.exit(1)
}

console.log(
  `\nWithin budget (${BUDGET_RAW_KB} KB raw / ${BUDGET_SERVED_KB} KB served).`,
)
