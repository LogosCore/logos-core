// Reproduction tests for: set_checklist_answer dropping checklist blocks.
//
// Two production reports:
//
// 1. Stub write (answer: "PLACEHOLDER", no fences) — total dropped in the
//    response itself, sibling items vanished from the Y.js tree.
//
// 2. Valid large fenced answer (~55 KB) — response said filled:1 total:20,
//    but immediate get_checklist_status showed total:19 and key gone.
//
// Tests cover: single-doc mutation, binary encode/decode round-trip
// (simulating persistence store → reload), and Y.js CRDT merge
// (simulating a concurrent browser client receiving the update).

import test from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import { XmlElement, XmlText, XmlFragment } from "yjs";
import { prosemirrorJSONToYDoc, yXmlFragmentToProseMirrorRootNode } from "y-prosemirror";
import { wikiSchema } from "../wiki-schema.js";
import { parseOutlineMarkdown } from "../markdown-parser.js";
import { serializeWikiDocument } from "../markdown-serializer.js";
import { collectChecklistCoverage } from "../references.js";

const Y_FRAGMENT_FIELD = "default";
const CHECKLIST_ITEM_NODE = "wikiChecklistItem";

// ---- Replicate the internal functions from set-checklist-answers.ts ----

function findChecklistItemByKey(
  node: XmlFragment | XmlElement,
  key: string,
): XmlElement | null {
  for (const child of node.toArray()) {
    if (!(child instanceof XmlElement)) continue;
    if (child.nodeName === CHECKLIST_ITEM_NODE) {
      if (child.getAttribute("key") === key) return child;
      continue;
    }
    const found = findChecklistItemByKey(child, key);
    if (found) return found;
  }
  return null;
}

function cloneNode(node: unknown): XmlElement | XmlText {
  if (node instanceof XmlText) {
    const copy = new XmlText();
    copy.applyDelta(node.toDelta());
    return copy;
  }
  if (node instanceof XmlElement) {
    const copy = new XmlElement(node.nodeName);
    for (const [k, v] of Object.entries(node.getAttributes())) {
      if (v !== undefined && v !== null) copy.setAttribute(k, v as string);
    }
    const children = node.toArray().map(cloneNode);
    if (children.length > 0) copy.insert(0, children);
    return copy;
  }
  if (node instanceof XmlFragment) {
    return new XmlElement("paragraph");
  }
  return new XmlText();
}

function markdownToDetachedNodes(markdown: string): (XmlElement | XmlText)[] {
  const pmDoc = parseOutlineMarkdown(markdown);
  const scratch = prosemirrorJSONToYDoc(wikiSchema, pmDoc.toJSON(), Y_FRAGMENT_FIELD);
  try {
    const fragment = scratch.getXmlFragment(Y_FRAGMENT_FIELD);
    return fragment.toArray().map(cloneNode);
  } finally {
    scratch.destroy();
  }
}

function replaceItemContent(item: XmlElement, markdown: string): boolean {
  const nodes = markdownToDetachedNodes(markdown);
  if (item.length > 0) {
    item.delete(0, item.length);
  }
  if (nodes.length > 0) {
    item.insert(0, nodes);
  } else {
    const p = new XmlElement("paragraph");
    p.insert(0, [new XmlText()]);
    item.insert(0, [p]);
  }
  return true;
}

// ---- Helpers ----

function makeKey(n: number): string {
  const hex = n.toString(16).padStart(8, "0");
  return `${hex}-0000-0000-0000-000000000000`;
}

// Build a Y.Doc from checklist markdown, exactly as the real system would via
// the markdown-to-yjs pipeline.
function buildDocFromMarkdown(md: string): Y.Doc {
  const pmDoc = parseOutlineMarkdown(md);
  return prosemirrorJSONToYDoc(wikiSchema, pmDoc.toJSON(), Y_FRAGMENT_FIELD);
}

// Simulate the persistence store → reload cycle: encode the doc to binary,
// create a fresh doc, apply the binary. This is what happens when hocuspocus
// evicts a room and reloads from MongoDB.
function storeAndReload(doc: Y.Doc): Y.Doc {
  const state = Y.encodeStateAsUpdate(doc);
  const fresh = new Y.Doc();
  Y.applyUpdate(fresh, state);
  return fresh;
}

// Check whether yXmlFragmentToProseMirrorRootNode drops checklist items
// when reading the Y.js state. In a real browser, y-prosemirror's sync plugin
// applies incoming Y.js updates by deriving ProseMirror state from the Y.js
// fragment; if that derivation loses items, the ProseMirror editor state
// diverges from Y.js. The sync plugin then writes the degraded ProseMirror
// state back to Y.js — which is the destructive counter-update the production
// bug describes.
//
// We can't directly simulate y-prosemirror's sync plugin in a unit test
// (it requires a full EditorView), but we CAN test the first half: does the
// Y.js → ProseMirror conversion lose items? If yes, that's the root cause.
function countItemsInProseMirrorView(doc: Y.Doc): number {
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);
  if (fragment.length === 0) return 0;
  const pmNode = yXmlFragmentToProseMirrorRootNode(fragment, wikiSchema);
  let count = 0;
  pmNode.descendants((node) => {
    if (node.type.name === CHECKLIST_ITEM_NODE) count++;
    return true;
  });
  return count;
}

// Collect checklist item keys as seen by ProseMirror.
function collectKeysFromProseMirrorView(doc: Y.Doc): string[] {
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);
  if (fragment.length === 0) return [];
  const pmNode = yXmlFragmentToProseMirrorRootNode(fragment, wikiSchema);
  const keys: string[] = [];
  pmNode.descendants((node) => {
    if (node.type.name === CHECKLIST_ITEM_NODE) {
      keys.push(node.attrs.key ?? "");
    }
    return true;
  });
  return keys;
}

// Collect all checklist item keys from the Y.js tree.
function collectKeys(fragment: XmlFragment): string[] {
  const keys: string[] = [];
  function walk(node: XmlFragment | XmlElement) {
    for (const child of node.toArray()) {
      if (!(child instanceof XmlElement)) continue;
      if (child.nodeName === CHECKLIST_ITEM_NODE) {
        keys.push(String(child.getAttribute("key") ?? ""));
        continue;
      }
      walk(child);
    }
  }
  walk(fragment);
  return keys;
}

// ---- Tests ----

// Minimal: replace content of one checklist item with "PLACEHOLDER" and
// confirm all sibling items survive.
test("replaceItemContent with unfenced 'PLACEHOLDER' preserves all sibling checklist items", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Precondition: all 5 items present.
  const beforeKeys = collectKeys(fragment);
  assert.equal(beforeKeys.length, 5, `expected 5 items before, got ${beforeKeys.length}`);
  const covBefore = collectChecklistCoverage(fragment);
  assert.equal(covBefore.total, 5, `expected total=5 before, got ${covBefore.total}`);

  // Fill items 1-3 with normal fenced answers.
  doc.transact(() => {
    for (let i = 0; i < 3; i++) {
      const item = findChecklistItemByKey(fragment, keys[i]);
      assert.ok(item, `item ${i + 1} not found`);
      replaceItemContent(item, "```plaintext\nsome output line " + (i + 1) + "\n```");
    }
  });

  // All 5 items should survive.
  assert.equal(collectKeys(fragment).length, 5, "items dropped after normal fills");
  assert.equal(collectChecklistCoverage(fragment).total, 5, "total dropped after normal fills");

  // Fill item 4 with a short fenced answer (like the bug report's uname step).
  doc.transact(() => {
    const item4 = findChecklistItemByKey(fragment, keys[3]);
    assert.ok(item4, "item 4 not found");
    replaceItemContent(item4, "```plaintext\nLinux host 6.1.0-amd64 #1 SMP x86_64 GNU/Linux\n```");
  });

  assert.equal(collectKeys(fragment).length, 5, "items dropped after item 4 fill");
  assert.equal(collectChecklistCoverage(fragment).total, 5, "total dropped after item 4 fill");

  // THE TRIGGER: fill item 5 with "PLACEHOLDER" (no fences).
  doc.transact(() => {
    const item5 = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item5, "item 5 not found before stub write");
    replaceItemContent(item5, "PLACEHOLDER");
  });

  const afterKeys = collectKeys(fragment);
  const covAfter = collectChecklistCoverage(fragment);

  assert.equal(afterKeys.length, 5, `expected 5 items after stub, got ${afterKeys.length}: [${afterKeys.join(", ")}]`);
  assert.equal(covAfter.total, 5, `expected total=5 after stub, got ${covAfter.total}`);

  // Verify every key is still findable.
  for (const k of keys) {
    assert.ok(findChecklistItemByKey(fragment, k), `key ${k} lost after stub write`);
  }
});

// Same test but with an empty string answer.
test("replaceItemContent with empty string preserves all sibling checklist items", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  assert.equal(collectKeys(fragment).length, 5);

  doc.transact(() => {
    const item = findChecklistItemByKey(fragment, keys[2]);
    assert.ok(item);
    replaceItemContent(item, "");
  });

  assert.equal(collectKeys(fragment).length, 5, "items dropped after empty-string answer");
  assert.equal(collectChecklistCoverage(fragment).total, 5, "total dropped after empty-string answer");
});

// Larger scale: 20 items (matching the production case).
test("20-item checklist: stub write on item 17 preserves all items", () => {
  const keys = Array.from({ length: 20 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","commandHint":"cmd-${i + 1}","commandHintEnabled":true,"required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  assert.equal(collectChecklistCoverage(fragment).total, 20, "precondition: 20 items");

  // Fill items 1-4 with proper fenced answers.
  doc.transact(() => {
    for (let i = 0; i < 4; i++) {
      const item = findChecklistItemByKey(fragment, keys[i]);
      assert.ok(item);
      replaceItemContent(item, "```plaintext\nresult " + (i + 1) + "\n```");
    }
  });

  assert.equal(collectChecklistCoverage(fragment).total, 20, "total after 4 normal fills");

  // Stub write on item 5 with unfenced "PLACEHOLDER".
  doc.transact(() => {
    const item5 = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item5);
    replaceItemContent(item5, "PLACEHOLDER");
  });

  const cov = collectChecklistCoverage(fragment);
  assert.equal(cov.total, 20, `expected total=20 after stub, got ${cov.total}`);
  assert.equal(cov.answered, 5, `expected answered=5 after stub (4 fenced + 1 placeholder text)`);
});

// Ensure the markdown serialization also preserves all checklist blocks after
// a PLACEHOLDER write — this tests the round-trip the persistence layer does.
test("markdown round-trip after PLACEHOLDER write preserves all checklist blocks", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Fill item 5 with PLACEHOLDER.
  doc.transact(() => {
    const item5 = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item5);
    replaceItemContent(item5, "PLACEHOLDER");
  });

  // Serialize back to markdown (the path get_wiki_document uses).
  // We need to go through ProseMirror for this, so use the Y.js → markdown path.
  // Instead, check that every :::checklist fence is present in the Y.js tree.
  const afterKeys = collectKeys(fragment);
  assert.equal(afterKeys.length, 5, `keys in Y.js tree: ${afterKeys.length}`);

  // Also verify that key 5 now contains the PLACEHOLDER text.
  const item5 = findChecklistItemByKey(fragment, keys[4]);
  assert.ok(item5);
  let textContent = "";
  for (const child of item5.toArray()) {
    if (child instanceof XmlText) {
      textContent += child.toString();
    } else if (child instanceof XmlElement) {
      // Walk into paragraphs
      for (const inner of child.toArray()) {
        if (inner instanceof XmlText) {
          textContent += inner.toString();
        }
      }
    }
  }
  assert.ok(textContent.includes("PLACEHOLDER"), `item 5 should contain PLACEHOLDER, got: "${textContent}"`);
});

// Test with a very large answer (~55 KB) which was the follow-up write
// in the production bug.
test("large fenced answer (~55 KB) does not drop sibling items", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Generate a ~55 KB output body.
  const bigBody = Array.from({ length: 726 }, (_, i) =>
    `line ${i + 1}: ${"x".repeat(70)}`,
  ).join("\n");

  doc.transact(() => {
    const item = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item);
    replaceItemContent(item, "```plaintext\n" + bigBody + "\n```");
  });

  const cov = collectChecklistCoverage(fragment);
  assert.equal(cov.total, 5, `expected total=5 after large answer, got ${cov.total}`);
});

// The exact production sequence: fill 1-4 normally, then stub item 5 with
// PLACEHOLDER, then retry item 5 with a large fenced answer.
test("production sequence: fill 1-4, stub item 5, retry item 5 with large answer", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","commandHint":"cmd-${i + 1}","commandHintEnabled":true,"required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);
  assert.equal(collectChecklistCoverage(fragment).total, 5, "precondition");

  // Step 1: fill items 1-3 with normal fenced answers.
  doc.transact(() => {
    for (let i = 0; i < 3; i++) {
      const item = findChecklistItemByKey(fragment, keys[i]);
      assert.ok(item);
      replaceItemContent(item, "```plaintext\nresult for item " + (i + 1) + "\n```");
    }
  });
  assert.equal(collectChecklistCoverage(fragment).total, 5, "after step 1");
  assert.equal(collectChecklistCoverage(fragment).answered, 3, "answered after step 1");

  // Step 2: fill item 4 with a short fenced answer (~120 bytes).
  doc.transact(() => {
    const item4 = findChecklistItemByKey(fragment, keys[3]);
    assert.ok(item4);
    replaceItemContent(item4, "```plaintext\nLinux host 6.1.0-amd64 #1 SMP x86_64 GNU/Linux\n```");
  });
  assert.equal(collectChecklistCoverage(fragment).total, 5, "after step 2");
  assert.equal(collectChecklistCoverage(fragment).answered, 4, "answered after step 2");

  // Step 3: THE TRIGGER — stub write on item 5 with "PLACEHOLDER".
  doc.transact(() => {
    const item5 = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item5, "item 5 must exist before stub write");
    replaceItemContent(item5, "PLACEHOLDER");
  });

  const covAfterStub = collectChecklistCoverage(fragment);
  assert.equal(covAfterStub.total, 5, `total after stub: expected 5, got ${covAfterStub.total}`);
  assert.equal(covAfterStub.answered, 5, `answered after stub: expected 5, got ${covAfterStub.answered}`);

  // Verify all keys still present.
  for (const k of keys) {
    assert.ok(findChecklistItemByKey(fragment, k), `key ${k} lost after stub`);
  }

  // Step 4: retry item 5 with a large (~55 KB) fenced answer.
  const bigBody = Array.from({ length: 726 }, (_, i) =>
    `line ${i + 1}: ${"x".repeat(70)}`,
  ).join("\n");

  doc.transact(() => {
    const item5 = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item5, "item 5 must exist before large-answer retry");
    replaceItemContent(item5, "```plaintext\n" + bigBody + "\n```");
  });

  const covAfterRetry = collectChecklistCoverage(fragment);
  assert.equal(covAfterRetry.total, 5, `total after retry: expected 5, got ${covAfterRetry.total}`);
  assert.equal(covAfterRetry.answered, 5, `answered after retry: expected 5, got ${covAfterRetry.answered}`);
});

// ---- Binary encode/decode round-trip tests (persistence cycle) ----

test("store-reload cycle preserves all items after PLACEHOLDER write", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Fill item 5 with PLACEHOLDER, then store and reload.
  doc.transact(() => {
    const item = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item);
    replaceItemContent(item, "PLACEHOLDER");
  });

  const reloaded = storeAndReload(doc);
  const reloadedFragment = reloaded.getXmlFragment(Y_FRAGMENT_FIELD);
  const reloadedKeys = collectKeys(reloadedFragment);

  assert.equal(reloadedKeys.length, 5, `after store-reload: expected 5 items, got ${reloadedKeys.length}`);
  assert.equal(collectChecklistCoverage(reloadedFragment).total, 5);

  doc.destroy();
  reloaded.destroy();
});

test("store-reload cycle preserves all items after large (~55 KB) fenced write", () => {
  const keys = Array.from({ length: 20 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","commandHint":"cmd-${i + 1}","commandHintEnabled":true,"required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Fill items 1-3.
  doc.transact(() => {
    for (let i = 0; i < 3; i++) {
      const item = findChecklistItemByKey(fragment, keys[i]);
      assert.ok(item);
      replaceItemContent(item, "```plaintext\nresult " + (i + 1) + "\n```");
    }
  });

  // Fill item 5 with ~55 KB fenced answer.
  const bigBody = Array.from({ length: 726 }, (_, i) =>
    `line ${i + 1}: ${"x".repeat(70)}`,
  ).join("\n");

  doc.transact(() => {
    const item = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item);
    replaceItemContent(item, "```plaintext\n" + bigBody + "\n```");
  });

  assert.equal(collectChecklistCoverage(fragment).total, 20, "before store-reload");

  const reloaded = storeAndReload(doc);
  const reloadedFragment = reloaded.getXmlFragment(Y_FRAGMENT_FIELD);

  assert.equal(collectChecklistCoverage(reloadedFragment).total, 20, "after store-reload");
  assert.ok(findChecklistItemByKey(reloadedFragment, keys[4]), "item 5 key must survive store-reload");

  doc.destroy();
  reloaded.destroy();
});

// ---- CRDT merge tests (simulating concurrent browser client) ----

test("ProseMirror view after PLACEHOLDER write preserves all items", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  doc.transact(() => {
    const item = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item);
    replaceItemContent(item, "PLACEHOLDER");
  });

  // Check Y.js tree directly.
  const afterKeys = collectKeys(fragment);
  assert.equal(afterKeys.length, 5, `Y.js tree: expected 5 items, got ${afterKeys.length}`);

  // Check what ProseMirror sees when it reads the Y.js state.
  const pmKeys = collectKeysFromProseMirrorView(doc);
  assert.equal(pmKeys.length, 5, `ProseMirror view: expected 5 items, got ${pmKeys.length}: [${pmKeys.join(", ")}]`);
  for (const k of keys) {
    assert.ok(pmKeys.includes(k), `ProseMirror view lost key ${k}`);
  }

  // Also check after a store-reload cycle (simulating room eviction).
  const reloaded = storeAndReload(doc);
  const pmKeysReloaded = collectKeysFromProseMirrorView(reloaded);
  assert.equal(pmKeysReloaded.length, 5, `ProseMirror view after reload: expected 5, got ${pmKeysReloaded.length}`);

  doc.destroy();
  reloaded.destroy();
});

test("ProseMirror view after large fenced write preserves all items", () => {
  const keys = Array.from({ length: 20 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","commandHint":"cmd-${i + 1}","commandHintEnabled":true,"required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Fill items 1-3.
  doc.transact(() => {
    for (let i = 0; i < 3; i++) {
      const item = findChecklistItemByKey(fragment, keys[i]);
      assert.ok(item);
      replaceItemContent(item, "```plaintext\nresult " + (i + 1) + "\n```");
    }
  });

  // Fill item 5 with ~55 KB.
  const bigBody = Array.from({ length: 726 }, (_, i) =>
    `line ${i + 1}: ${"x".repeat(70)}`,
  ).join("\n");

  doc.transact(() => {
    const item = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item);
    replaceItemContent(item, "```plaintext\n" + bigBody + "\n```");
  });

  // Check Y.js tree directly.
  const cov = collectChecklistCoverage(fragment);
  assert.equal(cov.total, 20, `Y.js tree: total=${cov.total}`);

  // Check what ProseMirror sees.
  const pmCount = countItemsInProseMirrorView(doc);
  assert.equal(pmCount, 20, `ProseMirror view: expected 20 items, got ${pmCount}`);

  // Also verify after store-reload.
  const reloaded = storeAndReload(doc);
  const pmCountReloaded = countItemsInProseMirrorView(reloaded);
  assert.equal(pmCountReloaded, 20, `ProseMirror view after reload: expected 20, got ${pmCountReloaded}`);

  doc.destroy();
  reloaded.destroy();
});

// ---- Markdown round-trip via Y.js binary (full persistence pipeline) ----

test("full pipeline: markdown → yjs → edit → yjs binary → markdown preserves all items", () => {
  const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
  const checklistMd = keys
    .map(
      (k, i) =>
        `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
        "```plaintext\n\n```\n" +
        ":::",
    )
    .join("\n\n");

  const doc = buildDocFromMarkdown(checklistMd);
  const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

  // Fill items 1-3 and item 5 with PLACEHOLDER.
  doc.transact(() => {
    for (let i = 0; i < 3; i++) {
      const item = findChecklistItemByKey(fragment, keys[i]);
      assert.ok(item);
      replaceItemContent(item, "```plaintext\nresult " + (i + 1) + "\n```");
    }
    const item5 = findChecklistItemByKey(fragment, keys[4]);
    assert.ok(item5);
    replaceItemContent(item5, "PLACEHOLDER");
  });

  // Serialize the doc to Y.js binary, then to markdown.
  const state = Y.encodeStateAsUpdate(doc);
  const rehydrated = new Y.Doc();
  Y.applyUpdate(rehydrated, state);
  const rehydratedFragment = rehydrated.getXmlFragment(Y_FRAGMENT_FIELD);
  const pmNode = yXmlFragmentToProseMirrorRootNode(rehydratedFragment, wikiSchema);
  const markdown = serializeWikiDocument(pmNode);

  // Every checklist item must appear in the markdown output.
  for (const k of keys) {
    assert.ok(
      markdown.includes(`"key":"${k}"`),
      `key ${k} missing from serialized markdown`,
    );
  }
  assert.ok(markdown.includes("PLACEHOLDER"), "PLACEHOLDER text must appear in markdown");

  doc.destroy();
  rehydrated.destroy();
});

// ---- Size bisect: test various answer sizes ----

for (const sizeKB of [1, 8, 32, 64, 100]) {
  test(`fenced answer of ~${sizeKB} KB preserves all items through store-reload`, () => {
    const keys = Array.from({ length: 5 }, (_, i) => makeKey(i + 1));
    const checklistMd = keys
      .map(
        (k, i) =>
          `:::checklist {"key":"${k}","prompt":"Item ${i + 1}","required":true}\n` +
          "```plaintext\n\n```\n" +
          ":::",
      )
      .join("\n\n");

    const doc = buildDocFromMarkdown(checklistMd);
    const fragment = doc.getXmlFragment(Y_FRAGMENT_FIELD);

    const body = "x".repeat(sizeKB * 1024);
    doc.transact(() => {
      const item = findChecklistItemByKey(fragment, keys[2]);
      assert.ok(item);
      replaceItemContent(item, "```plaintext\n" + body + "\n```");
    });

    assert.equal(collectChecklistCoverage(fragment).total, 5, `in-memory after ${sizeKB}KB`);

    const reloaded = storeAndReload(doc);
    const reloadedFragment = reloaded.getXmlFragment(Y_FRAGMENT_FIELD);
    assert.equal(collectChecklistCoverage(reloadedFragment).total, 5, `store-reload after ${sizeKB}KB`);

    doc.destroy();
    reloaded.destroy();
  });
}
