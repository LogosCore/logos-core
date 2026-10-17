import { describe, test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { DiagramError } from "../diagram-language.js";
import { applyDiagram } from "../diagram-apply.js";
import { readScene } from "../diagram-read.js";
import { aliasesOf, flowchartToDiagram, mermaidId, writeMermaid } from "../mermaid-flowchart.js";
import { isMermaid, parseMermaidText } from "../mermaid-parse.js";
import { drawSequence, parseSequence } from "../sequence-layout.js";
import { normalizeElements, type NormalizedElement } from "../drawing-elements.js";
import { DRAWING_ROOT_KEY } from "../drawing-projection.js";

function room() {
  const doc = new Y.Doc();
  return { doc, map: doc.getMap<NormalizedElement>(DRAWING_ROOT_KEY) };
}
const live = (map: Y.Map<NormalizedElement>) => [...map.values()].filter((el) => !el.isDeleted);

async function draw(map: Y.Map<NormalizedElement>, text: string) {
  const { diagram, warnings } = await flowchartToDiagram(text, aliasesOf(readScene(live(map))));
  let result!: ReturnType<typeof applyDiagram>;
  (map.doc as Y.Doc).transact(() => {
    result = applyDiagram(map, diagram, "merge");
  });
  return { ...result, warnings: [...warnings, ...result.warnings] };
}

const NETWORK = `flowchart LR
  subgraph lan["Internal — 10.0.0.0/24"]
    ws1["WS-0142<br/>Windows 11"]:::workstation
    dc01["DC01<br/>10.0.5.10"]:::domain-controller
  end
  class lan dmz
  op["Operator"]:::attacker
  kms["HSM"]
  kms@{ icon: "key-round" }
  class kms red,target
  class dc01 compromised
  op e1@-->|phishing| ws1
  ws1 e2@-.->|"pass-the-hash"| dc01
  dc01 ==> kms
  kms --o op
  class e1 exploit
  class e2 lateral,curved,head-triangle
`;

describe("Mermaid flowcharts", () => {
  test("map onto cards, zones and styled connections", async () => {
    const { diagram, warnings } = await flowchartToDiagram(NETWORK);
    assert.deepEqual(warnings, []);
    const dc = diagram.nodes.get("dc01")!;
    assert.deepEqual([dc.kind, dc.title, dc.detail, dc.zone, dc.addBadges], ["domain-controller", "DC01", "10.0.5.10", "lan", ["compromised"]]);
    const kms = diagram.nodes.get("kms")!;
    assert.deepEqual([kms.icon, kms.color, kms.addBadges], ["key-round", "red", ["target"]]);
    assert.equal(diagram.zones.get("lan")?.kind, "dmz");
    assert.equal(diagram.direction, "right");
    const [e1, e2, thick, circle] = diagram.edges;
    assert.equal(e1.style, "exploit");
    assert.deepEqual([e2.style, e2.route, e2.head, e2.stroke], ["lateral", "curved", "triangle", "dotted"]);
    assert.equal(thick.stroke, "thick");
    assert.equal(circle.head, "circle");
  });

  // Naming a node in a link must not retitle it to its id.
  test("a node only mentioned in a link keeps its title", async () => {
    const { map } = room();
    await draw(map, NETWORK);
    await draw(map, "flowchart LR\n  dc01 --> extra[\"Extra\"]:::server");
    assert.equal(readScene(live(map)).nodes.get("dc01")?.title, "DC01");
  });

  test("reads back as Mermaid that draws the same thing", async () => {
    const { map } = room();
    await draw(map, NETWORK);
    const back = writeMermaid(readScene(live(map)));
    assert.ok(isMermaid(back));
    const again = await draw(map, back);
    assert.deepEqual([again.created, again.updated, again.connected, again.removed], [0, 0, 0, 0]);
  });

  test("a shape with no id of its own is written under an alias, and found again by it", async () => {
    const { map } = room();
    for (const el of normalizeElements([{ type: "rectangle", id: "hand-drawn-1", x: 0, y: 0, label: "Jump host" }])) map.set(el.id, el);
    const back = writeMermaid(readScene(live(map)));
    assert.match(back, new RegExp(mermaidId("hand-drawn-1")));
    await draw(map, `flowchart LR\n  ${mermaidId("hand-drawn-1")} -->|ssh| t["Target"]:::server`);
    const scene = readScene(live(map));
    assert.equal(scene.edges[0]?.from, "hand-drawn-1");
  });

  test("an unknown icon is refused with icons that fit", async () => {
    await assert.rejects(flowchartToDiagram('flowchart LR\n  a["A"]\n  a@{ icon: "vault" }'), (err: unknown) => {
      assert.ok(err instanceof DiagramError);
      assert.match(err.problems[0].message, /Icons that fit: lock-keyhole/);
      return true;
    });
  });

  test("a Mermaid syntax error comes back with its line", async () => {
    await assert.rejects(flowchartToDiagram("flowchart LR\n  a -->\n"), (err: unknown) => {
      assert.ok(err instanceof DiagramError);
      assert.match(err.problems[0].message, /Mermaid could not parse this/);
      return true;
    });
  });

  test("a class that means nothing here is a warning, not a failure", async () => {
    const { warnings } = await flowchartToDiagram('flowchart LR\n  a["A"]:::sparkly');
    assert.match(warnings.join(), /"sparkly" on a means nothing here/);
  });

  test("%% lines carry what Mermaid cannot say", async () => {
    const { map } = room();
    await draw(map, NETWORK);
    await draw(map, "flowchart LR\n  %% remove op\n  %% dc01 -compromised\n  %% style clean");
    const scene = readScene(live(map));
    assert.equal(scene.nodes.has("op"), false);
    assert.deepEqual(scene.nodes.get("dc01")?.badges, []);
  });

  test("a top-down flowchart is laid out downward", async () => {
    const { diagram } = await flowchartToDiagram("flowchart TB\n  a --> b");
    assert.equal(diagram.direction, "down");
  });
});

const SEQUENCE = `sequenceDiagram
  title OIDC sign-in
  autonumber
  %% spa: web-app "Logos SPA"
  box Browser
    actor user as Operator
    participant spa as Logos SPA
  end
  participant core as Logos core
  user->>spa: Sign in
  spa->>+core: GET /login
  core-->>-spa: 302
  core->>core: check state
  Note over spa,core: sealed cookie
  alt no role
    core-->>spa: error
  else ok
    core-->>spa: cookies
  end
`;

describe("Mermaid sequence diagrams", () => {
  test("draw participants as cards, a row per message, numbered", async () => {
    const { map } = room();
    const parsed = await parseSequence(SEQUENCE);
    let result!: ReturnType<typeof drawSequence>;
    map.doc!.transact(() => {
      result = drawSequence(map, readScene(live(map)), SEQUENCE, parsed);
    });
    assert.deepEqual([result.id, result.participants, result.messages], ["oidc-sign-in", 3, 6]);
    const texts = live(map).filter((el) => el.type === "text").map((el) => String(el.originalText ?? el.text));
    assert.ok(texts.includes("1. Sign in"), texts.join(" | "));
    assert.ok(texts.includes("6. cookies"));
    assert.ok(texts.some((t) => t.startsWith("alt")), "the alt frame is labelled");
    const cards = live(map).filter((el) => (el.customData as { logos?: { shape?: string } })?.logos?.shape === "card:web-app");
    assert.ok(cards.length > 0, "the %% line made spa a web-app card");
    const arrows = live(map).filter((el) => el.type === "arrow");
    assert.equal(arrows.filter((a) => a.strokeStyle === "dashed").length, 3, "replies are dashed");
  });

  test("read back as its source, and redrawn in place when sent again", async () => {
    const { map } = room();
    const parsed = await parseSequence(SEQUENCE);
    map.doc!.transact(() => drawSequence(map, readScene(live(map)), SEQUENCE, parsed));
    const first = readScene(live(map));
    assert.equal(first.sequences.get("oidc-sign-in")?.source, SEQUENCE);
    assert.equal(first.nodes.size, 0, "its parts are not read as nodes");
    assert.deepEqual(first.others, []);
    const changed = SEQUENCE.replace("GET /login", "GET /auth/oidc/login");
    const reparsed = await parseSequence(changed);
    map.doc!.transact(() => drawSequence(map, readScene(live(map)), changed, reparsed));
    const second = readScene(live(map));
    assert.equal(second.sequences.size, 1);
    assert.equal(Math.round(second.sequences.get("oidc-sign-in")!.box.x), Math.round(first.sequences.get("oidc-sign-in")!.box.x));
    assert.ok(live(map).some((el) => String(el.originalText ?? el.text) === "2. GET /auth/oidc/login"));
  });

  test("a %% line for a participant that is not there is refused", async () => {
    await assert.rejects(parseSequence("sequenceDiagram\n  %% nobody: server\n  a->>b: hi"), DiagramError);
  });
});

describe("parsing", () => {
  test("tells Mermaid from the diagram language", () => {
    assert.equal(isMermaid("%% comment\nflowchart LR\n a --> b"), true);
    assert.equal(isMermaid("sequenceDiagram\n a->>b: x"), true);
    assert.equal(isMermaid('dc01: server "DC01"'), false);
  });

  // Mermaid's sequence parser reaches for window.CSS; the stand-in must not
  // outlive the parse, or the rest of the sidecar would think it runs in a
  // browser.
  test("leaves no window behind, even with parses running at once", async () => {
    await Promise.all([
      parseMermaidText("sequenceDiagram\n  box Browser\n  participant a\n  end\n  a->>b: x"),
      parseMermaidText("flowchart LR\n  a --> b"),
    ]);
    assert.equal(typeof (globalThis as { window?: unknown }).window, "undefined");
  });
});
