// Convert a wiki document's Y.js binary state into ProseMirror JSON.
//
// The sibling of yjs-to-markdown.ts for exports that render the page
// themselves: the HTML bundle hands this JSON to the editor's own Tiptap
// extensions in an offline viewer, so the page comes out with the app's
// markup rather than through a second renderer. Same fragment, same schema,
// so what an agent reads as markdown and what the viewer shows are one tree.

import * as Y from "yjs";
import { yXmlFragmentToProseMirrorRootNode } from "y-prosemirror";
import { wikiSchema } from "./wiki-schema.js";
import { Y_FRAGMENT_FIELD } from "./yjs-to-markdown.js";

/** An empty document, as ProseMirror JSON. What a page nobody has opened
 * yet — no state at all — renders as. */
export const EMPTY_PROSEMIRROR_DOC = { type: "doc", content: [{ type: "paragraph" }] };

/**
 * Convert a Y.js update binary to ProseMirror JSON. Throws if the bytes are
 * not a valid Y.js update; callers treat that as a per-document failure.
 */
export function yjsUpdateToProseMirrorJSON(update: Uint8Array): Record<string, unknown> {
  const ydoc = new Y.Doc();
  try {
    Y.applyUpdate(ydoc, update);
    const fragment = ydoc.getXmlFragment(Y_FRAGMENT_FIELD);
    if (fragment.length === 0) return EMPTY_PROSEMIRROR_DOC;
    return yXmlFragmentToProseMirrorRootNode(fragment, wikiSchema).toJSON();
  } finally {
    ydoc.destroy();
  }
}
