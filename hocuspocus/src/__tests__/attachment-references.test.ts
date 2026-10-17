import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { attachmentReferencesOf } from "../attachment-references.js";

const IMG = "3f2504e0-4f89-41d3-9a0c-0305e82c3301";
const FILE = "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d";
const OTHER = "1f0e3dad-9990-4b5e-bd3a-6a6a6a6a6a6a";

test("a prose body's images and files", () => {
  const doc = new Y.Doc();
  const image = new Y.XmlElement("image");
  image.setAttribute("src", `/api/v1/wiki/images/${IMG}`);
  const file = new Y.XmlElement("wikiFile");
  file.setAttribute("fileId", FILE);
  doc.getXmlFragment("default").insert(0, [image, file]);

  assert.deepEqual(attachmentReferencesOf(doc, "document"), {
    imageReferences: [IMG],
    fileReferences: [FILE],
  });
});

test("a drawing's images, and not its erased ones", () => {
  const doc = new Y.Doc();
  const map = doc.getMap("excalidraw");
  map.set("a", { id: "a", type: "image", fileId: IMG, version: 1 });
  map.set("b", { id: "b", type: "image", fileId: OTHER, version: 2, isDeleted: true });

  assert.deepEqual(attachmentReferencesOf(doc, "drawing"), {
    imageReferences: [IMG],
    fileReferences: [],
  });
});

// A textarea-era body and the Markdown of a never-opened page have no nodes
// to walk; their attachment URLs are matched as text.
test("plain bodies are matched by URL", () => {
  const legacy = new Y.Doc();
  legacy.getText("content").insert(0, `see ![](/api/v1/wiki/images/${IMG.toUpperCase()})`);

  assert.deepEqual(
    attachmentReferencesOf(legacy, "document", `[report](/api/v1/wiki/files/${FILE})`),
    { imageReferences: [IMG], fileReferences: [FILE] },
  );
});

test("an empty body references nothing", () => {
  assert.deepEqual(attachmentReferencesOf(new Y.Doc(), "document"), {
    imageReferences: [],
    fileReferences: [],
  });
});
