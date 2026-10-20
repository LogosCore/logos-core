import { describe, test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { readScene } from "../diagram-read.js";
import { drawSequence, parseSequence } from "../sequence-layout.js";
import type { NormalizedElement } from "../drawing-elements.js";
import { DRAWING_ROOT_KEY } from "../drawing-projection.js";
import { textCollisions } from "../text-overlap.js";

const live = (map: Y.Map<NormalizedElement>) => [...map.values()].filter((el) => !el.isDeleted);
const part = (el: NormalizedElement) => (el.customData as { logos?: { part?: string } } | undefined)?.logos?.part;

async function draw(source: string) {
  const doc = new Y.Doc();
  const map = doc.getMap<NormalizedElement>(DRAWING_ROOT_KEY);
  const parsed = await parseSequence(source);
  doc.transact(() => drawSequence(map, readScene(live(map)), source, parsed));
  return live(map);
}

// Rebuilt from a drawing an agent made on a production instance. Its message
// labels ran down across their arrows, two self-message labels spilled into
// the next row, and consecutive frames had stepped edges.
const DISTRIBUTOR = `sequenceDiagram
  title Mail.ru Distributor Protocol
  participant req as Requester
  participant api as Mail.ru API
  participant dist as mailru-distributor
  participant pm as Payload Manager
  Note over dist,pm: Startup — control channel
  dist->>pm: Connect WSS /api/v1/ws/distributor (Bearer token)
  pm-->>dist: add {id: strategyUUID, data: /path}
  dist->>dist: Store path map strategyId to path
  loop keepalive ~5s
    dist->>pm: ping
    pm-->>dist: pong
  end
  Note over req,api: Requester places order as draft
  req->>api: POST /messages/draft subject=requestId text=/path + meta lines
  api-->>req: draft id
  Note over api,dist: Discover request
  loop every 5s
    dist->>api: POST /messages/status folder=500001
    api-->>dist: draft list
    dist->>api: GET /messages/message id=draft
    api-->>dist: body text + date
  end
  dist->>dist: Match line1 to known path
  Note over dist,pm: Fulfill via /distribute
  loop every 10s until HTTP 200
    dist->>api: Re-read draft (refresh body + date)
    api-->>dist: latest meta + X-Draft-Date
    dist->>pm: GET /distribution-strategy/{id}/distribute
    Note right of dist: Headers:<br/>Distributor-Method GET<br/>X-Request-Id<br/>X-Draft-Date UTC<br/>X-Meta-*
    alt strategy not ready
      pm-->>dist: 404 (inactive / no downloads) + Distribution log
    else ready
      pm-->>dist: 200 octet-stream payload
    end
  end
  Note over api,dist: Deliver file on draft
  dist->>api: POST /messages/attaches/add (temp id)
  api-->>dist: attach id
  dist->>api: POST /messages/draft source.draft=oldId + attaches
  api-->>dist: new draft id flags.attach=true
  Note over req,api: Requester waits for attachment
  loop until flags.attach=true
    req->>api: GET /messages/message id=draft
    api-->>req: draft (check attaches / flags.attach)
  end
  req->>api: Download payload.bin from CDN
  api-->>req: file bytes
`;

describe("sequence layout", () => {
  test("no label runs across an arrow, another label or a border", async () => {
    const scene = await draw(DISTRIBUTOR);
    const byID = new Map(scene.map((el) => [el.id, el]));
    const collisions = textCollisions(scene).map(
      (c) => `${JSON.stringify(byID.get(c.text)?.text)} ${c.kind} ${part(byID.get(c.other)!) ?? c.other}` +
        (c.kind === "text" ? ` ${JSON.stringify(byID.get(c.other)?.text)}` : ""),
    );
    assert.deepEqual(collisions, []);
  });

  test("a message label sits wholly above its arrow, however many lines it wraps to", async () => {
    const scene = await draw(DISTRIBUTOR);
    const arrows = scene.filter((el) => part(el) === "message");
    const labels = scene.filter((el) => part(el) === "message-text");
    assert.equal(arrows.length, labels.length);
    const multi = labels.filter((l) => String(l.text).includes("\n"));
    assert.ok(multi.length >= 5, "the long labels do wrap");
    arrows.forEach((arrow, i) => {
      const label = labels[i];
      if ((arrow.points as number[][]).length > 2) return; // self-message
      const bottom = Number(label.y) + Number(label.height);
      assert.ok(bottom <= Number(arrow.y), `${JSON.stringify(label.text)} ends at ${bottom}, arrow at ${arrow.y}`);
    });
  });

  test("frames that follow one another line up; only nesting insets them", async () => {
    const scene = await draw(DISTRIBUTOR);
    const frames = scene.filter((el) => part(el) === "frame");
    assert.equal(frames.length, 5);
    for (const f of frames) assert.equal(f.roughness, 0, "frames are drawn straight");
    // keepalive (dist..pm) and every 10s (api..pm) both end at Payload
    // Manager's column; the alt nested inside the latter is inset.
    const right = (el: NormalizedElement) => Math.round(Number(el.x) + Number(el.width));
    const [keepalive, , tenSeconds, alt] = frames.sort((a, b) => Number(a.y) - Number(b.y));
    assert.equal(right(keepalive), right(tenSeconds));
    assert.ok(right(alt) < right(tenSeconds));
  });

  test("a note is only as tall as the lines it is drawn with", async () => {
    const scene = await draw(DISTRIBUTOR);
    const notes = scene.filter((el) => part(el) === "note");
    const byID = new Map(scene.map((el) => [el.id, el]));
    for (const note of notes) {
      const text = byID.get((note.boundElements as { id: string }[])[0].id)!;
      const lines = String(text.text).split("\n").length;
      assert.equal(Number(note.height), Math.max(40, Number(text.height) + 20), `note ${JSON.stringify(text.text)}`);
      assert.equal(Number(text.height), Math.ceil(lines * 15 * 1.25));
    }
  });
});

test("a nested frame widened by a self-message stays inside its parent", async () => {
  const scene = await draw(`sequenceDiagram
  participant a as A
  participant b as B
  loop outer
    alt inner
      b->>b: a long self-message that reaches well past the last column
    end
  end
`);
  const [outer, inner] = scene.filter((el) => part(el) === "frame").sort((p, q) => Number(p.y) - Number(q.y));
  const label = scene.find((el) => part(el) === "message-text")!;
  const right = (el: NormalizedElement) => Number(el.x) + Number(el.width);
  assert.ok(right(inner) >= right(label), "the inner frame takes in the words");
  assert.ok(right(outer) > right(inner), "and the outer frame takes in the inner");
  assert.ok(Number(outer.x) < Number(inner.x));
});
