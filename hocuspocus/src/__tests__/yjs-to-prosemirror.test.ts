// The HTML bundle renders this JSON with the editor's own Tiptap extensions,
// so it has to be exactly the tree the editor stores, custom nodes included.

import test from "node:test";
import assert from "node:assert/strict";
import { markdownToYjsUpdate } from "../markdown-to-yjs.js";
import { EMPTY_PROSEMIRROR_DOC, yjsUpdateToProseMirrorJSON } from "../yjs-to-prosemirror.js";

interface PMNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: PMNode[];
}

function types(node: PMNode, out: string[] = []): string[] {
  out.push(node.type);
  for (const child of node.content ?? []) types(child, out);
  return out;
}

test("renders a page as the editor's node tree, chips and notices included", () => {
  const id = "11111111-2222-3333-4444-555555555555";
  const md = [
    "# Heading",
    "",
    `See [page](logos://doc/${id}) and **bold**.`,
    "",
    ":::warning",
    "Careful",
    ":::",
    "",
    "- [ ] todo",
  ].join("\n");
  const doc = yjsUpdateToProseMirrorJSON(markdownToYjsUpdate(md)) as unknown as PMNode;

  assert.equal(doc.type, "doc");
  const seen = types(doc);
  for (const t of ["heading", "paragraph", "wikiDocumentReference", "wikiNotice", "taskList", "taskItem"]) {
    assert.ok(seen.includes(t), `expected a ${t} node, got ${seen.join(", ")}`);
  }
  const chip = JSON.stringify(doc).includes(id);
  assert.ok(chip, "the chip keeps its document id for the exporter to resolve");
});

test("an empty state is an empty document, not an error", () => {
  const empty = yjsUpdateToProseMirrorJSON(markdownToYjsUpdate(""));
  assert.deepEqual(empty, EMPTY_PROSEMIRROR_DOC);
});
