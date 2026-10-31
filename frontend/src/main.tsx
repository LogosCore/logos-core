import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import App from "./App.tsx"
import { warmFontsWhenIdle } from "./lib/font-warmup"

// Point Excalidraw at fonts served from our own origin.
//
// Set here, at bootstrap, rather than inside the lazily-loaded drawing chunk:
// a static import is hoisted above any assignment in the same module, so
// setting it next to the import would run *after* the library initialises.
// Costs nothing on pages that never open a canvas — it is one string.
//
// Without it Excalidraw fetches its fonts from a CDN, which is wrong for an
// air-gapped deployment. Note the library keeps the CDN as a fallback even
// when this is set, so a path that does not resolve degrades into a silent
// internet dependency rather than a visible failure — verify offline, not by
// reading the config. The path is served by the excalidrawAssets() plugin in
// vite.config.ts and the two must agree.
declare global {
  interface Window {
    EXCALIDRAW_ASSET_PATH?: string | string[]
  }
}
window.EXCALIDRAW_ASSET_PATH = "/excalidraw-assets/"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)

// The Cyrillic and full-width fonts, ahead of the first text that needs
// them: fetched then, they made the whole page blink (lib/font-warmup.ts).
warmFontsWhenIdle()
