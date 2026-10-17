// Load stored Y.js bytes into a document.
//
// A page or backup saved before it was ever opened in an editor has no
// content_state, and Go sends that as zero bytes. Y.applyUpdate rejects zero
// bytes ("Unexpected end of array") rather than treating them as the empty
// document they stand for, so every route that takes stored state goes
// through here instead.

import * as Y from "yjs";

export function applyStoredState(doc: Y.Doc, state: Uint8Array): void {
  if (state.length === 0) return;
  Y.applyUpdate(doc, state);
}
