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

// ---------------------------------------------------------------------------
// Timeline, gantt, state, mindmap

import { DRAWN_TYPES, mermaidType } from "../mermaid-common.js";
import { drawTimeline } from "../timeline-layout.js";
import { drawGantt } from "../gantt-layout.js";
import { mindmapToDiagram, stateToDiagram } from "../mermaid-graphs.js";

const textOf = (el: NormalizedElement) => String(el.originalText ?? el.text ?? "");
const tagPart = (el: NormalizedElement) => (el.customData as { logos?: { part?: string; shape?: string } } | undefined)?.logos;

async function drawFixed(map: Y.Map<NormalizedElement>, source: string, draw: typeof drawTimeline | typeof drawGantt) {
  const db = (await parseMermaidText(source)).db;
  let result!: ReturnType<typeof draw>;
  map.doc!.transact(() => {
    result = draw(map, readScene(live(map)), source, db);
  });
  return result;
}

async function drawGraph(map: Y.Map<NormalizedElement>, source: string, convert: typeof stateToDiagram) {
  const db = (await parseMermaidText(source)).db;
  const { diagram, warnings } = convert(source, db);
  let result!: ReturnType<typeof applyDiagram>;
  map.doc!.transact(() => {
    result = applyDiagram(map, diagram, "merge");
  });
  return { ...result, warnings: [...warnings, ...result.warnings] };
}

const TIMELINE = `timeline
  title Operation Nightjar
  section Week 1
    3 Mar : Kick-off : scope signed
    5 Mar : External scan
  section Week 2
    12 Mar : Foothold
`;

describe("Mermaid timelines", () => {
  test("periods on an axis, events beneath, sections above", async () => {
    const { map } = room();
    const result = await drawFixed(map, TIMELINE, drawTimeline);
    assert.deepEqual([result.id, result.periods, result.events], ["operation-nightjar", 3, 4]);
    const texts = live(map).map(textOf);
    for (const word of ["3 Mar", "Kick-off", "scope signed", "Week 1", "Week 2", "Foothold"]) assert.ok(texts.includes(word), word);
    assert.equal(live(map).filter((el) => tagPart(el)?.part === "section").length, 2);
  });

  test("read back as its source, and redrawn in place", async () => {
    const { map } = room();
    await drawFixed(map, TIMELINE, drawTimeline);
    const first = readScene(live(map)).sequences.get("operation-nightjar")!;
    assert.deepEqual([first.type, first.source], ["timeline", TIMELINE]);
    await drawFixed(map, TIMELINE.replace("Foothold", "Foothold on WS-0142"), drawTimeline);
    const second = readScene(live(map)).sequences.get("operation-nightjar")!;
    assert.equal(Math.round(second.box.x), Math.round(first.box.x));
    assert.ok(live(map).map(textOf).includes("Foothold on WS-0142"));
    assert.ok(!live(map).map(textOf).includes("Foothold"), "the old version is gone");
  });
});

describe("Mermaid gantt charts", () => {
  const GANTT = `gantt
  title Plan
  dateFormat YYYY-MM-DD
  section Recon
  OSINT :done, osint, 2026-03-03, 3d
  Scan :active, scan, after osint, 2d
  section Access
  Phishing :crit, phish, 2026-03-10, 5d
  Report :milestone, rep, 2026-03-20, 0d
`;

  test("a bar per task at its dates, a diamond per milestone", async () => {
    const { map } = room();
    const result = await drawFixed(map, GANTT, drawGantt);
    assert.equal(result.tasks, 4);
    const bars = live(map).filter((el) => tagPart(el)?.part === "bar");
    assert.equal(bars.length, 3);
    assert.equal(live(map).filter((el) => tagPart(el)?.part === "milestone").length, 1);
    const [osint, scan, phish] = ["osint", "scan", "phish"].map((id) => bars.find((b) => (b.customData as { logos: { node?: string } }).logos.node === id)!);
    // Scan starts where OSINT ends, and is two thirds as long.
    assert.ok(Math.abs(Number(scan.x) - (Number(osint.x) + Number(osint.width))) < 0.5);
    assert.ok(Math.abs(Number(scan.width) / Number(osint.width) - 2 / 3) < 0.01);
    assert.equal(phish.strokeColor, "#e03131", "critical is red");
    assert.ok(live(map).map(textOf).includes("3 Mar – 5 Mar"));
    assert.equal(readScene(live(map)).sequences.get("plan")?.type, "gantt");
  });
});

describe("Mermaid state diagrams", () => {
  const STATE = `stateDiagram-v2
  [*] --> Recon
  Recon --> Access : target found
  state Access {
    [*] --> Phish
    Phish --> Phish : blocked, retry
    Phish --> Foothold
  }
  Access --> [*]
  note right of Recon : passive only
  class Foothold compromised
`;

  test("states, a composite as a zone, markers, a loop, a note and a badge", async () => {
    const { map } = room();
    const result = await drawGraph(map, STATE, stateToDiagram);
    assert.deepEqual(result.warnings, []);
    const scene = readScene(live(map));
    assert.ok(scene.zones.has("Access"));
    assert.equal(scene.nodes.get("Phish")?.zone, "Access");
    assert.deepEqual(scene.nodes.get("Foothold")?.badges, ["compromised"]);
    assert.deepEqual([...scene.nodes.values()].filter((n) => n.kind === "start").length, 2);
    assert.ok(scene.edges.some((e) => e.from === "Recon" && e.to === "Access" && e.label === "target found"), "into the composite");
    assert.ok(scene.edges.some((e) => e.from === "Phish" && e.to === "Phish"), "the loop");
    assert.ok([...scene.nodes.values()].some((n) => n.kind === "note" && n.title === "passive only"));
    assert.deepEqual(scene.others, []);
  });
});

describe("Mermaid mindmaps", () => {
  const MINDMAP = `mindmap
  root((Attack surface))
    External
      VPN portal
      Web apps
        ::icon(fa fa-globe)
    People
      Helpdesk
`;

  test("a tree, each branch in its own colour, joined without arrowheads", async () => {
    const { map } = room();
    await drawGraph(map, MINDMAP, mindmapToDiagram);
    const scene = readScene(live(map));
    assert.equal(scene.nodes.size, 6);
    assert.equal(scene.nodes.get("vpn_portal")?.color, scene.nodes.get("external")?.color);
    assert.notEqual(scene.nodes.get("external")?.color, scene.nodes.get("people")?.color);
    assert.equal(scene.nodes.get("web_apps")?.icon, "globe");
    assert.ok(scene.edges.every((e) => e.arrow === "--" && e.route === "curved"));
  });

  test("sent twice, changes nothing the second time", async () => {
    const { map } = room();
    await drawGraph(map, MINDMAP, mindmapToDiagram);
    const again = await drawGraph(map, MINDMAP, mindmapToDiagram);
    assert.deepEqual([again.created, again.updated, again.connected], [0, 0, 0]);
  });
});

describe("diagram types", () => {
  test("are recognised by name, drawn or not", () => {
    assert.equal(mermaidType("gantt\n  title x"), "gantt");
    assert.equal(mermaidType("stateDiagram-v2\n  [*] --> a"), "state");
    assert.equal(mermaidType("classDiagram\n  A <|-- B"), "class");
    assert.equal(mermaidType('dc01: server "DC01"'), null);
    assert.deepEqual([...DRAWN_TYPES], ["flowchart", "sequence", "state", "mindmap", "timeline", "gantt"]);
  });
});
