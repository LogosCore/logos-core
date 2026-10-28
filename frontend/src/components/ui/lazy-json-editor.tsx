import { preloadable, useLoaded } from "@/lib/preloadable"
import { cn } from "@/lib/utils"
import type { JsonEditorProps } from "./json-editor"

// The JSON editor's code (CodeMirror) is a chunk of its own: fetched when an
// editor is first shown, or ahead of that with preloadJsonEditor() once a
// screen that has one opens. Use the editor through LazyJsonEditor.

const jsonEditorCode = preloadable(() => import("./json-editor"))

/** Fetches the JSON editor's code in the background. A failure stays quiet; showing the editor reports it. */
export function preloadJsonEditor() {
  jsonEditorCode.load().catch(() => {})
}

/** The JSON editor (json-editor.tsx), shown once its code is loaded. */
export function LazyJsonEditor(props: JsonEditorProps) {
  const code = useLoaded(jsonEditorCode)
  if (!code) {
    return (
      <div className={cn("flex items-center justify-center text-sm text-muted-foreground", props.className)}>
        Loading the editor…
      </div>
    )
  }
  return <code.JsonEditor {...props} />
}
