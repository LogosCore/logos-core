import { create } from "zustand"
import type { Editor } from "@tiptap/react"

/**
 * The tiptap instance of the page currently on screen.
 *
 * The header and the editor are siblings under WikiEditorPane, so the header
 * has no path to the editor through props — and the outline it offers is a
 * read of the editor's document. One instance is enough: only one wiki page
 * is mounted at a time, and a pane that has none (a drawing) simply leaves
 * this null, which is how the header knows not to offer an outline.
 *
 * Deliberately not part of the wiki store: everything there is serializable
 * UI state, and this is a live object with a destroy lifecycle. Keeping it
 * apart means nothing can accidentally persist or snapshot it.
 */
interface ActiveEditorState {
  editor: Editor | null
  setEditor: (editor: Editor | null) => void
}

const useActiveEditorStore = create<ActiveEditorState>()((set) => ({
  editor: null,
  setEditor: (editor) => set({ editor }),
}))

/** Subscribe to the live editor. Null while none is mounted or ready. */
export function useActiveWikiEditor(): Editor | null {
  return useActiveEditorStore((s) => s.editor)
}

/** Publish (or, with null, retract) the live editor. Called by WikiEditor. */
export function setActiveWikiEditor(editor: Editor | null): void {
  useActiveEditorStore.getState().setEditor(editor)
}
