// Entry point of the HTML bundle's viewer: assets/viewer.js. See
// core/pkg/wikitransfer/htmlbundle for the archive it is part of.

import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "@/index.css"
import "@/components/wiki/wiki-editor.css"
import "./viewer.css"
import { ViewerApp, ViewerProblem as Problem } from "./viewer-app"
import { readTree, SUPPORTED_VERSION } from "./viewer-data"
import { installFileFetch } from "./viewer-files"
import { ThemeProvider } from "@/components/theme-provider"

installFileFetch()

const tree = readTree()
const root = createRoot(document.getElementById("root")!)
root.render(
  <StrictMode>
    {/* The app's theme provider, so everything that reads the theme the way
        the app does — file previews, toasts — follows the viewer's toggle. */}
    <ThemeProvider storageKey="logos-viewer-theme">
      {!tree ? (
        <Problem message="This export's data (data/tree.js) is missing. Unpack the whole zip and open index.html from inside it." />
      ) : tree.version > SUPPORTED_VERSION ? (
        <Problem message="This export was written by a newer Logos than its viewer understands." />
      ) : (
        <ViewerApp tree={tree} />
      )}
    </ThemeProvider>
  </StrictMode>,
)
