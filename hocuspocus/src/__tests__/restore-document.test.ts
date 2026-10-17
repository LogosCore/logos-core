import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { exactKey, restoreDocumentInto } from "../restore-document.js";

/** A top-level block: a paragraph or heading holding some text. */
function block(text: string, name = "paragraph", attrs: Record<string, string> = {}): Y.XmlElement {
  const el = new Y.XmlElement(name);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  const t = new Y.XmlText();
  t.insert(0, text);
  el.insert(0, [t]);
  return el;
}

function fragmentOf(doc: Y.Doc): Y.XmlFragment {
  return doc.getXmlFragment("default");
}

/** The page's blocks as text, for assertions. */
function texts(doc: Y.Doc): string[] {
  return fragmentOf(doc)
    .toArray()
    .map((n) => (n instanceof Y.XmlElement ? n.toArray().map(String).join("") : String(n)));
}

/** A page as a backup saw it, then edited past that point. */
function editedSinceBackup(): { room: Y.Doc; backup: Uint8Array } {
  const room = new Y.Doc();
  fragmentOf(room).insert(0, [block("Scope", "heading"), block("Two hosts in scope."), block("Notes")]);
  const backup = Y.encodeStateAsUpdate(room);

  // After the snapshot: the middle paragraph is rewritten and a new one added.
  const f = fragmentOf(room);
  f.delete(1, 1);
  f.insert(1, [block("Twelve hosts in scope.")]);
  f.insert(3, [block("Typed after the backup.")]);
  return { room, backup };
}

// The bug, reproduced: writing the backup bytes over the row is undone by a
// browser that reconnects holding the edited state. A Y.js merge is a union,
// so the restored page carries the old and the new paragraphs side by side.
test("writing the backup bytes over the row is undone by a reconnecting browser", () => {
  const { room, backup } = editedSinceBackup();

  const restoredRow = new Y.Doc();
  Y.applyUpdate(restoredRow, backup);
  Y.applyUpdate(restoredRow, Y.encodeStateAsUpdate(room));

  assert.deepEqual(texts(restoredRow), [
    "Scope",
    "Twelve hosts in scope.",
    "Notes",
    "Typed after the backup.",
  ]);
});

test("the room ends up holding exactly the backup's body", () => {
  const { room, backup } = editedSinceBackup();

  const result = room.transact(() => restoreDocumentInto(room, backup));

  assert.deepEqual(texts(room), ["Scope", "Two hosts in scope.", "Notes"]);
  assert.deepEqual(result, { blocks: 3, source: "fragment" });
});

// Blocks the backup shares with the page are not rewritten, so whoever is
// typing in one keeps their caret and nothing they see jumps.
test("blocks the backup shares with the page are left in place", () => {
  const { room, backup } = editedSinceBackup();
  const heading = fragmentOf(room).get(0);

  room.transact(() => restoreDocumentInto(room, backup));

  assert.equal(fragmentOf(room).get(0), heading);
});

// And unlike the byte write, the restore survives the same reconnect: the
// browser's copy of the deleted paragraphs loses to the restore's deletions.
test("a browser reconnecting with the pre-restore state does not undo it", () => {
  const { room, backup } = editedSinceBackup();
  const browser = new Y.Doc();
  Y.applyUpdate(browser, Y.encodeStateAsUpdate(room));

  room.transact(() => restoreDocumentInto(room, backup));
  Y.applyUpdate(room, Y.encodeStateAsUpdate(browser));
  Y.applyUpdate(browser, Y.encodeStateAsUpdate(room));

  assert.deepEqual(texts(room), ["Scope", "Two hosts in scope.", "Notes"]);
  assert.deepEqual(texts(browser), texts(room));
});

// apply-markdown's splice ignores attributes the server schema does not
// declare, because Markdown cannot carry them. A backup does, so a block that
// differs only in one of them must still come back as the backup had it.
test("a block differing only in an undeclared attribute is restored exactly", () => {
  const room = new Y.Doc();
  fragmentOf(room).insert(0, [block("x = 1", "codeBlock", { language: "js", blockId: "before" })]);
  const backup = Y.encodeStateAsUpdate(room);
  const f = fragmentOf(room);
  f.delete(0, 1);
  f.insert(0, [block("x = 1", "codeBlock", { language: "js", blockId: "after" })]);

  room.transact(() => restoreDocumentInto(room, backup));

  const restored = fragmentOf(room).get(0) as Y.XmlElement;
  assert.equal(restored.getAttribute("blockId"), "before");
});

test("inline marks are part of what is restored", () => {
  const plain = new Y.Doc();
  fragmentOf(plain).insert(0, [block("important")]);
  const bold = new Y.Doc();
  const el = block("important");
  fragmentOf(bold).insert(0, [el]);
  (el.get(0) as Y.XmlText).format(0, 9, { bold: {} });

  assert.notEqual(exactKey(fragmentOf(plain).get(0)), exactKey(fragmentOf(bold).get(0)));
});

// A textarea-era backup holds its body as plain text under "content", with
// no editor fragment at all.
test("a textarea-era backup is restored from its legacy text", () => {
  const legacy = new Y.Doc();
  legacy.getText("content").insert(0, "Old notes\n\nSecond paragraph");
  const { room } = editedSinceBackup();

  const result = room.transact(() => restoreDocumentInto(room, Y.encodeStateAsUpdate(legacy)));

  assert.deepEqual(texts(room), ["Old notes", "Second paragraph"]);
  assert.equal(result.source, "legacy-text");
});

// A backup taken before anyone opened the page has no state at all, only the
// Markdown the page was created with.
test("a backup with no state falls back to its stored Markdown", () => {
  const { room } = editedSinceBackup();

  const result = room.transact(() =>
    restoreDocumentInto(room, Y.encodeStateAsUpdate(new Y.Doc()), "# Title\n\nBody text"),
  );

  assert.deepEqual(texts(room), ["Title", "Body text"]);
  assert.equal(result.source, "markdown");
});

// The editor migrates legacy text into an empty fragment on open, so text
// left behind would reappear the moment a restore emptied the page.
test("the room's own legacy text is cleared", () => {
  const room = new Y.Doc();
  room.getText("content").insert(0, "never migrated");

  room.transact(() => restoreDocumentInto(room, Y.encodeStateAsUpdate(new Y.Doc())));

  assert.equal(room.getText("content").toString(), "");
});

test("an empty backup empties the page", () => {
  const { room } = editedSinceBackup();

  const result = room.transact(() => restoreDocumentInto(room, Y.encodeStateAsUpdate(new Y.Doc())));

  assert.deepEqual(texts(room), []);
  assert.deepEqual(result, { blocks: 0, source: "empty" });
});

// What Go actually sends for a backup that has no content_state: zero bytes,
// not an encoded empty document. Y.applyUpdate rejects zero bytes outright,
// which made exactly the backups the Markdown fallback exists for fail.
test("a backup stored with no state at all falls back to its Markdown", () => {
  const { room } = editedSinceBackup();

  const result = room.transact(() => restoreDocumentInto(room, new Uint8Array(), "Only text"));

  assert.deepEqual(texts(room), ["Only text"]);
  assert.equal(result.source, "markdown");
});
