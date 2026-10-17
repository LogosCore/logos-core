import { describe, test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { DiagramError, parseDiagram } from "../diagram-language.js";
import { applyDiagram } from "../diagram-apply.js";
import { readScene, sceneToText, type Box } from "../diagram-read.js";
import { buildCard, BUILTIN_SHAPES, ICON_NAMES } from "../drawing-catalogue.js";
import { skeletonsToElements } from "../drawing-skeleton.js";
import { normalizeElements, type NormalizedElement } from "../drawing-elements.js";
import { DRAWING_ROOT_KEY } from "../drawing-projection.js";
import { inPaintOrder } from "../drawing-order.js";
import { tagOf } from "../drawing-catalogue.js";
import { routeElbow } from "../arrow-routing.js";

function room(): { doc: Y.Doc; map: Y.Map<NormalizedElement> } {
  const doc = new Y.Doc();
  return { doc, map: doc.getMap<NormalizedElement>(DRAWING_ROOT_KEY) };
}

function live(map: Y.Map<NormalizedElement>): NormalizedElement[] {
  return [...map.values()].filter((el) => !el.isDeleted);
}

function apply(map: Y.Map<NormalizedElement>, text: string, mode: "merge" | "replace" = "merge") {
  let result!: ReturnType<typeof applyDiagram>;
  (map.doc as Y.Doc).transact(() => {
    result = applyDiagram(map, parseDiagram(text), mode);
  });
  return result;
}

const read = (map: Y.Map<NormalizedElement>) => sceneToText(readScene(live(map)));

function overlapping(a: Box, b: Box): boolean {
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

const NETWORK = `
zone internal "Internal — 10.0.0.0/24" network {
  ws142: workstation "WS-0142" "Windows 11"
  dc01: domain-controller "DC01" "10.0.5.10" +compromised
}
attacker: attacker "Operator"
attacker -> ws142 "phishing" [exploit]
ws142 -> dc01 "pass-the-hash" [lateral]
`;

// ---------------------------------------------------------------------------

describe("the diagram language", () => {
  test("reads nodes, zones, connections and badges", () => {
    const d = parseDiagram(NETWORK);
    assert.deepEqual([...d.nodes.keys()], ["ws142", "dc01", "attacker"]);
    const dc = d.nodes.get("dc01");
    assert.equal(dc?.kind, "domain-controller");
    assert.equal(dc?.title, "DC01");
    assert.equal(dc?.detail, "10.0.5.10");
    assert.deepEqual(dc?.addBadges, ["compromised"]);
    assert.equal(dc?.zone, "internal");
    assert.equal(d.zones.get("internal")?.kind, "network");
    assert.deepEqual(
      d.edges.map((e) => [e.from, e.arrow, e.to, e.label, e.style]),
      [
        ["attacker", "->", "ws142", "phishing", "exploit"],
        ["ws142", "->", "dc01", "pass-the-hash", "lateral"],
      ],
    );
  });

  test("takes what models write out of habit: Mermaid arrows, comments, escapes", () => {
    const d = parseDiagram(`# a comment\na --> b "say \\"hi\\""  # trailing\na: server\nb: server`);
    assert.equal(d.edges[0].arrow, "->");
    assert.equal(d.edges[0].label, 'say "hi"');
  });

  test("reads directives, removals and badge changes", () => {
    const d = parseDiagram(`direction down\nstyle clean\nremove old1\nremove a -> b\ndc01 +flag -compromised`);
    assert.equal(d.direction, "down");
    assert.equal(d.style, "clean");
    assert.deepEqual(d.removals.map((r) => r.kind), ["item", "edge"]);
    assert.deepEqual(d.nodes.get("dc01")?.addBadges, ["flag"]);
    assert.deepEqual(d.nodes.get("dc01")?.removeBadges, ["compromised"]);
  });

  // The point of the format for small models: every mistake at once, by line,
  // with what to write instead.
  test("reports every problem with its line, and suggests the kind meant", () => {
    assert.throws(
      () => parseDiagram(`dc01: domain-controler "DC01"\nws: workstation\na -> b [sideways]\nzone z "Z" {`),
      (err: unknown) => {
        assert.ok(err instanceof DiagramError);
        assert.deepEqual(err.problems.map((p) => p.line), [1, 3, 4]);
        assert.match(err.problems[0].message, /Did you mean domain-controller/);
        assert.match(err.problems[1].message, /Styles: plain, traffic/);
        assert.match(err.problems[2].message, /never closed/);
        return true;
      },
    );
  });

  test("a written diagram parses back to the same diagram", () => {
    const { map } = room();
    apply(map, NETWORK);
    const text = read(map);
    const again = parseDiagram(text);
    assert.deepEqual([...again.nodes.keys()].sort(), ["attacker", "dc01", "ws142"]);
    assert.equal(again.edges.length, 2);
  });
});

// ---------------------------------------------------------------------------

describe("reading a drawing as a diagram", () => {
  test("a card built from the catalogue reads as one node", () => {
    const { map } = room();
    const card = buildCard({ kind: "server", title: "SRV01", detail: "10.0.0.5", style: "sketch", badges: ["flag"], node: "srv01" });
    for (const el of skeletonsToElements(card.elements, { x: 0, y: 0 })) map.set(el.id, el);

    const scene = readScene(live(map));
    assert.equal(scene.nodes.size, 1);
    assert.deepEqual(scene.others, []);
    assert.equal(read(map), 'srv01: server "SRV01" "10.0.0.5" +flag');
  });

  // Placed from the Shapes panel: tagged, but with no diagram id.
  test("a card with no diagram id answers to its frame's element id", () => {
    const { map } = room();
    const elements = skeletonsToElements(BUILTIN_SHAPES.find((s) => s.key === "card-database")!.build("sketch"), { x: 0, y: 0 });
    for (const el of elements) map.set(el.id, el);
    assert.equal(read(map), `${elements[0].id}: database "Database" "MSSQL · 1433"`);
  });

  test("a copied card does not share its original's id", () => {
    const { map } = room();
    for (const x of [0, 400]) {
      const card = buildCard({ kind: "server", title: "A", detail: "", style: "sketch", node: "a" });
      for (const el of skeletonsToElements(card.elements, { x, y: 0 })) map.set(el.id, el);
    }
    const ids = [...readScene(live(map)).nodes.keys()];
    assert.equal(ids.length, 2);
    assert.equal(ids[0], "a");
    assert.notEqual(ids[1], "a");
  });

  test("hand-drawn labelled shapes and the arrow between them read as nodes and a connection", () => {
    const { map } = room();
    const [a, aLabel, b, bLabel] = normalizeElements([
      { type: "rectangle", id: "boxA", x: 0, y: 0, label: "Jump host" },
      { type: "ellipse", id: "boxB", x: 300, y: 0, label: "Target" },
    ]);
    for (const el of [a, aLabel, b, bLabel]) map.set(el.id, el);
    const [arrow] = normalizeElements([{ type: "arrow", id: "ar", startBinding: "boxA", endBinding: "boxB", points: [[0, 0], [200, 0]] }]);
    map.set(arrow.id, arrow);
    const [note] = normalizeElements([{ type: "text", x: 0, y: 300, text: "TODO: scope" }]);
    map.set(note.id, note);

    const scene = readScene(live(map));
    // A person's arrow is straight; the read says so once, for all of them.
    assert.equal(read(map), 'arrows straight\nboxA: box "Jump host"\nboxB: ellipse "Target"\nboxA -> boxB');
    assert.deepEqual(scene.others.map((o) => [o.type, o.text]), [["text", "TODO: scope"]]);
  });

  test("an arrow with a loose end is listed apart, not dropped", () => {
    const { map } = room();
    const [arrow] = normalizeElements([{ type: "arrow", id: "loose", x: 0, y: 0 }]);
    map.set(arrow.id, arrow);
    assert.deepEqual(readScene(live(map)).others.map((o) => o.id), ["loose"]);
  });

  test("a badge dropped onto a card from the panel counts as that card's", () => {
    const { map } = room();
    const card = buildCard({ kind: "server", title: "A", detail: "", style: "sketch", node: "a" });
    for (const el of skeletonsToElements(card.elements, { x: 0, y: 0 })) map.set(el.id, el);
    const badge = BUILTIN_SHAPES.find((s) => s.key === "badge-compromised")!.build("sketch");
    for (const el of skeletonsToElements(badge, { x: card.width - 20, y: -15 })) map.set(el.id, el);
    assert.equal(read(map), 'a: server "A" +compromised');
  });
});

// ---------------------------------------------------------------------------

describe("writing a diagram", () => {
  test("draws it, and reads back as what was written", () => {
    const { map } = room();
    const result = apply(map, NETWORK);
    assert.deepEqual([result.created, result.connected], [4, 2]);
    assert.equal(
      read(map),
      [
        'zone internal "Internal — 10.0.0.0/24" network {',
        '  ws142: workstation "WS-0142" "Windows 11"',
        '  dc01: domain-controller "DC01" "10.0.5.10" +compromised',
        "}",
        'attacker: attacker "Operator"',
        'attacker -> ws142 "phishing" [exploit]',
        'ws142 -> dc01 "pass-the-hash" [lateral]',
      ].join("\n"),
    );
  });

  test("nothing it draws overlaps, and zones hold their nodes", () => {
    const { map } = room();
    apply(map, NETWORK + "\nfs01: file-share \"FS01\"\ndc01 -> fs01\nweb: web-app \"Portal\"\nattacker -> web");
    const scene = readScene(live(map));
    const boxes = [...scene.nodes.values()].map((n) => n.box);
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) assert.ok(!overlapping(boxes[i], boxes[j]), `nodes ${i} and ${j} overlap`);
    }
    for (const node of scene.nodes.values()) {
      if (node.id === "ws142" || node.id === "dc01") assert.equal(node.zone, "internal");
    }
  });

  test("the same text twice changes nothing the second time", () => {
    const { map } = room();
    apply(map, NETWORK);
    const result = apply(map, NETWORK);
    assert.deepEqual([result.created, result.updated, result.connected, result.removed], [0, 0, 0, 0]);
  });

  // An operator's arrangement survives every agent edit.
  test("adding to a drawing never moves what is already on it", () => {
    const { map } = room();
    apply(map, NETWORK);
    const before = new Map([...readScene(live(map)).nodes.values()].map((n) => [n.id, n.box]));
    apply(map, 'db01: database "SQL01"\ndc01 -> db01 [creds]\nweb: web-app "Portal"');
    const after = readScene(live(map));
    for (const [id, box] of before) assert.deepEqual(after.nodes.get(id)?.box.x, box.x, id);
    for (const [id, box] of before) assert.deepEqual(after.nodes.get(id)?.box.y, box.y, id);
    const db = after.nodes.get("db01")?.box as Box;
    for (const node of after.nodes.values()) if (node.id !== "db01") assert.ok(!overlapping(db, node.box), node.id);
  });

  test("a new node is placed beside the node it connects to", () => {
    const { map } = room();
    apply(map, NETWORK);
    apply(map, 'db01: database "SQL01"\ndc01 -> db01');
    const scene = readScene(live(map));
    const dc = scene.nodes.get("dc01")!.box;
    const db = scene.nodes.get("db01")!.box;
    assert.ok(Math.hypot(db.x - dc.x, db.y - dc.y) < 600, "far from what it connects to");
  });

  // A card that changes is rebuilt; its arrows must not come off.
  test("retitling a card keeps every arrow attached to it", () => {
    const { map } = room();
    apply(map, NETWORK);
    const frameBefore = readScene(live(map)).nodes.get("dc01")!.frameId;
    apply(map, 'dc01: "DC01 (PDC)" "10.0.5.10, 10.0.6.10"');
    const scene = readScene(live(map));
    const dc = scene.nodes.get("dc01")!;
    assert.equal(dc.frameId, frameBefore);
    assert.equal(dc.title, "DC01 (PDC)");
    assert.deepEqual(dc.badges, ["compromised"]);
    const edge = scene.edges.find((e) => e.to === "dc01");
    assert.ok(edge, "the connection into dc01 survived");
    const frame = map.get(dc.frameId)!;
    assert.ok((frame.boundElements as { id: string }[]).some((b) => b.id === edge.id), "the frame still lists its arrow");
  });

  test("a rebuilt frame outranks the copy every canvas holds", () => {
    const { map } = room();
    apply(map, NETWORK);
    const id = readScene(live(map)).nodes.get("dc01")!.frameId;
    const before = map.get(id)!.version;
    apply(map, "dc01 +flag");
    assert.ok(map.get(id)!.version > before);
  });

  test("connections can reach shapes drawn by hand, by their element id", () => {
    const { map } = room();
    for (const el of normalizeElements([{ type: "rectangle", id: "handBox", x: 0, y: 0, label: "Jump host" }])) map.set(el.id, el);
    apply(map, 'srv: server "SRV"\nhandBox -> srv "ssh"');
    assert.match(read(map), /handBox -> srv "ssh"/);
  });

  test("retitling a hand-drawn shape changes its words and nothing else", () => {
    const { map } = room();
    for (const el of normalizeElements([{ type: "rectangle", id: "handBox", x: 7, y: 9, width: 333, height: 99, label: "Old" }])) {
      map.set(el.id, el);
    }
    apply(map, 'handBox: "New"');
    const box = map.get("handBox")!;
    assert.deepEqual([box.x, box.y, box.width, box.height], [7, 9, 333, 99]);
    assert.match(read(map), /handBox: box "New"/);
  });

  test("restyling a connection changes the arrow rather than adding one", () => {
    const { map } = room();
    apply(map, NETWORK);
    const result = apply(map, 'ws142 -> dc01 "pass-the-ticket" [creds]');
    assert.deepEqual([result.connected, result.updated], [0, 1]);
    assert.match(read(map), /ws142 -> dc01 "pass-the-ticket" \[creds\]/);
    assert.equal(readScene(live(map)).edges.length, 2);
  });

  test("removing a node takes its connections with it", () => {
    const { map } = room();
    apply(map, NETWORK);
    apply(map, "remove ws142");
    const scene = readScene(live(map));
    assert.equal(scene.nodes.has("ws142"), false);
    assert.equal(scene.edges.length, 0);
    assert.deepEqual(scene.others, []);
  });

  test("a new node declared in a zone on the canvas goes inside it", () => {
    const { map } = room();
    apply(map, NETWORK);
    apply(map, 'zone internal {\n  srv9: server "SRV9"\n}');
    const scene = readScene(live(map));
    assert.equal(scene.nodes.get("srv9")?.zone, "internal");
  });

  // Checked before anything is written: Y.js does not roll back a throwing
  // transaction, so a late check would leave half an edit behind.
  test("a reference to a node that does not exist changes nothing", () => {
    const { map } = room();
    apply(map, NETWORK);
    const before = read(map);
    for (const mode of ["merge", "replace"] as const) {
      assert.throws(() => apply(map, 'new1: server "N"\nnew1 -> nosuch', mode), (err: unknown) => {
        assert.ok(err instanceof DiagramError);
        assert.match(err.problems[0].message, /no node "nosuch"/);
        return true;
      });
      assert.equal(read(map), before, mode);
    }
  });

  test("a new node needs a kind", () => {
    const { map } = room();
    assert.throws(() => apply(map, 'srv: "SRV"'), /needs a kind/);
  });

  test("replace starts from an empty canvas", () => {
    const { map } = room();
    apply(map, NETWORK);
    apply(map, 'only: server "Only"', "replace");
    assert.equal(read(map), 'only: server "Only"');
  });

  test("a clean diagram reads back as clean", () => {
    const { map } = room();
    apply(map, 'style clean\na: server "A"');
    assert.equal(read(map), 'style clean\na: server "A"');
  });
});

// ---------------------------------------------------------------------------

describe("colours and icons", () => {
  test("are read from a node line, by name or #hex, and can be cleared", () => {
    const d = parseDiagram('a: box "A" color=red icon=key-round\nb: server color=#C2255C\nc color=default icon=none');
    assert.equal(d.nodes.get("a")?.color, "red");
    assert.equal(d.nodes.get("a")?.icon, "key-round");
    assert.equal(d.nodes.get("b")?.color, "#c2255c");
    assert.equal(d.nodes.get("c")?.color, null);
    assert.equal(d.nodes.get("c")?.icon, null);
  });

  test("an unknown colour lists the palette", () => {
    assert.throws(() => parseDiagram('a: box "A" color=purple'), /Colours: gray, red, .*#rrggbb/);
  });

  // An agent names an icon by what it means; spelling suggestions would offer
  // "bolt" for "vault".
  test("an unknown icon suggests icons that mean it", () => {
    assert.throws(() => parseDiagram('a: box "A" icon=vault'), /Icons that fit: lock-keyhole, key-round, shield/);
    assert.throws(() => parseDiagram('a: box "A" icon=siem'), /Icons that fit: chart-line/);
    assert.throws(() => parseDiagram('a: box "A" icon=zzqx'), /Name it by what it shows/);
  });

  test("a box with an icon is a card of its own, and reads back with both", () => {
    const { map } = room();
    apply(map, 'kms: box "HSM" "Thales Luna" icon=key-round color=red +target');
    assert.equal(read(map), 'kms: box "HSM" "Thales Luna" +target color=red icon=key-round');
    const scene = readScene(live(map));
    assert.equal(scene.nodes.get("kms")?.box.height, buildCard({ kind: "server", title: "x", detail: "", style: "sketch" }).height);
  });

  test("a built-in card can be recoloured and given another icon", () => {
    const { map } = room();
    apply(map, 'c2: c2-server "Redirector" color=pink icon=globe-lock');
    assert.equal(read(map), 'c2: c2-server "Redirector" color=pink icon=globe-lock');
  });

  test("recolouring keeps the node's arrows attached", () => {
    const { map } = room();
    apply(map, 'a: box "A" icon=lock\nb: server "B"\na -> b');
    const frame = readScene(live(map)).nodes.get("a")!.frameId;
    apply(map, "a color=green");
    const scene = readScene(live(map));
    assert.equal(scene.nodes.get("a")!.frameId, frame);
    assert.equal(scene.nodes.get("a")!.color, "green");
    assert.equal(scene.edges.length, 1);
  });

  test("taking the icon off a box makes it a plain box again, and its badges go with it", () => {
    const { map } = room();
    apply(map, 'a: box "A" "detail" icon=lock +flag');
    const result = apply(map, "a icon=none");
    assert.equal(read(map), 'a: box "A"');
    assert.match(result.warnings.join(), /only cards carry badges/);
  });

  test("color=default puts a card's own colour back", () => {
    const { map } = room();
    apply(map, 'a: server "A" color=red');
    apply(map, "a color=default");
    assert.equal(read(map), 'a: server "A"');
  });

  test("a plain shape takes a colour, and black gets light text", () => {
    const { map } = room();
    apply(map, 'a: ellipse "Okta" color=grape\nb: diamond "Crown" color=black');
    assert.equal(read(map), 'a: ellipse "Okta" color=grape\nb: diamond "Crown" color=black');
    const crown = live(map).find((el) => el.type === "text" && el.text === "Crown");
    assert.equal(crown?.strokeColor, "#ffffff");
  });

  test("an icon on a shape that cannot show one is left off, with a warning", () => {
    const { map } = room();
    const result = apply(map, 'a: diamond "D" icon=skull');
    assert.equal(read(map), 'a: diamond "D"');
    assert.match(result.warnings.join(), /only cards and boxes take an icon/);
  });

  test("every icon an agent can name builds", () => {
    for (const name of ICON_NAMES) {
      assert.doesNotThrow(() => buildCard({ kind: "box", title: "t", detail: "", style: "sketch", icon: name }), name);
    }
  });
});

// Elements written by the server carry no Excalidraw index, so paint order
// falls to z and then to element id. With one z for a whole card that was id
// order, which is random, and a card's white frame painted over its own title
// and icon: the card showed up blank.
describe("paint order", () => {
  test("a card paints its frame first and its words last, whatever its ids", () => {
    for (let run = 0; run < 20; run++) {
      const { map } = room();
      apply(map, 'a: box "Title" "detail" icon=key-round color=red +flag');
      const parts = inPaintOrder(live(map)).map((el) => tagOf(el)?.part);
      assert.equal(parts[0], "frame");
      assert.ok(parts.indexOf("tile") < parts.indexOf("icon"), "tile under icon");
      assert.ok(parts.indexOf("icon") < parts.indexOf("title"), "icon under title");
    }
  });

  test("an arrow's label paints over its arrow, and under the cards", () => {
    const { map } = room();
    apply(map, 'a: server "A"\nb: server "B"\na -> b "link"');
    const order = inPaintOrder(live(map));
    const arrow = order.findIndex((el) => el.type === "arrow");
    const label = order.findIndex((el) => el.type === "text" && el.text === "link");
    const firstCard = order.findIndex((el) => tagOf(el)?.part === "frame" && tagOf(el)?.shape === "card:server");
    assert.ok(arrow < label && label < firstCard, `${arrow} ${label} ${firstCard}`);
  });
});

// Excalidraw draws a label exactly as stored and only re-wraps it when the
// shape is edited or resized, so a long note written as one line ran out of
// the note on one line.
describe("long labels", () => {
  const LONG = "Root causes: macro-enabled mail allowed, helpdesk is local admin on all workstations, Veeam stores domain creds.";

  test("are stored wrapped to fit their shape", () => {
    const { map } = room();
    apply(map, `n1: note ${JSON.stringify(LONG)}`);
    const note = live(map).find((el) => el.type === "rectangle")!;
    const label = live(map).find((el) => el.type === "text")!;
    const lines = String(label.text).split("\n");
    assert.ok(lines.length > 3, `${lines.length} lines`);
    for (const line of lines) assert.ok(line.length * 16 * 0.6 <= Number(note.width), line);
    assert.equal(label.originalText, LONG);
    assert.ok(Number(label.height) <= Number(note.height), "the text fits in the note");
  });

  test("read back as written, so sending them again changes nothing", () => {
    const { map } = room();
    apply(map, `n1: note ${JSON.stringify(LONG)}\na: server "A"\nn1 -> a "a fairly long connection label that has to wrap somewhere"`);
    assert.match(read(map), /n1: note "Root causes: macro-enabled mail allowed, helpdesk/);
    const again = apply(map, read(map));
    assert.deepEqual([again.created, again.updated, again.connected], [0, 0, 0]);
  });

  test("an element label from edit_wiki_drawing is wrapped too", () => {
    const [box, label] = normalizeElements([{ type: "rectangle", width: 120, height: 80, label: LONG }]);
    assert.ok(String(label.text).includes("\n"));
    assert.equal(label.originalText, LONG);
    assert.equal(box.width, 120);
  });
});

// ---------------------------------------------------------------------------

describe("arrow routes and heads", () => {
  type Pt = [number, number];
  const arrowOf = (map: Y.Map<NormalizedElement>) => live(map).find((el) => el.type === "arrow")!;
  const globalPoints = (el: NormalizedElement) => (el.points as Pt[]).map(([x, y]) => [Number(el.x) + x, Number(el.y) + y] as Pt);
  const orthogonal = (pts: Pt[]) => pts.slice(1).every((p, i) => p[0] === pts[i][0] || p[1] === pts[i][1]);

  test("the language reads routes, heads and the arrows default", () => {
    const d = parseDiagram("arrows curved\na -> b [lateral straight head=triangle tail=circle-outline]\na -> c head=diamond");
    assert.equal(d.arrows, "curved");
    assert.deepEqual([d.edges[0].style, d.edges[0].route, d.edges[0].head, d.edges[0].tail], ["lateral", "straight", "triangle", "circle-outline"]);
    assert.equal(d.edges[1].head, "diamond");
    assert.throws(() => parseDiagram("a -> b [zigzag]"), /Routes: elbow, curved, straight/);
    assert.throws(() => parseDiagram("a -> b head=spear"), /Arrowheads: arrow, triangle/);
  });

  test("a new connection is an elbow: only horizontal and vertical runs, bound with fixed points", () => {
    const { map } = room();
    apply(map, 'a: server "A"\nb: server "B"\nc: server "C"\na -> b\na -> c');
    for (const arrow of live(map).filter((el) => el.type === "arrow")) {
      assert.equal(arrow.elbowed, true);
      assert.ok(orthogonal(globalPoints(arrow)), JSON.stringify(arrow.points));
      for (const end of ["startBinding", "endBinding"] as const) {
        const fp = (arrow[end] as { fixedPoint?: number[] }).fixedPoint;
        assert.ok(Array.isArray(fp) && fp.every((v) => v >= 0 && v <= 1), `${end} ${JSON.stringify(fp)}`);
      }
    }
    assert.match(read(map), /^a -> b$/m);
  });

  test("an elbow goes around a shape in its way", () => {
    const from = { x: 0, y: 0, width: 200, height: 76 };
    const to = { x: 700, y: 0, width: 200, height: 76 };
    const wall = { x: 350, y: -60, width: 200, height: 200 };
    const route = routeElbow({ from, to, obstacles: [wall] });
    assert.ok(orthogonal(route.points));
    for (let i = 1; i < route.points.length; i++) {
      const [ax, ay] = route.points[i - 1];
      const [bx, by] = route.points[i];
      for (let t = 0; t <= 1; t += 0.1) {
        const x = ax + (bx - ax) * t;
        const y = ay + (by - ay) * t;
        assert.ok(!(x > wall.x && x < wall.x + wall.width && y > wall.y && y < wall.y + wall.height), `crosses at ${x},${y}`);
      }
    }
  });

  test("arrows sharing a side leave from different points", () => {
    const { map } = room();
    apply(map, 'direction down\nhub: server "Hub"\na: server "A"\nb: server "B"\nc: server "C"\nhub -> a\nhub -> b\nhub -> c');
    const starts = live(map).filter((el) => el.type === "arrow").map((el) => globalPoints(el)[0].join());
    assert.equal(new Set(starts).size, 3);
  });

  test("curved and straight connections, and both read back", () => {
    const { map } = room();
    apply(map, 'a: server "A"\nb: server "B"\na -> b "one" [curved]\nb -> a "two" [straight]');
    const [one, two] = ["one", "two"].map((t) => {
      const label = live(map).find((el) => el.text === t)!;
      return map.get(String(label.containerId))!;
    });
    assert.deepEqual(one.roundness, { type: 2 });
    assert.equal((one.points as Pt[]).length, 3);
    assert.equal(two.elbowed, false);
    assert.equal(two.roundness, null);
    // One of each: the read names one route as the default and the other on
    // its line, and sending the read back changes nothing.
    assert.equal(read(map), 'arrows curved\na: server "A"\nb: server "B"\na -> b "one"\nb -> a "two" [straight]');
    const again = apply(map, read(map));
    assert.deepEqual([again.updated, again.connected], [0, 0]);
  });

  test("arrowheads are written in Excalidraw's names and read back in ours", () => {
    const { map } = room();
    apply(map, 'a: database "A"\nb: database "B"\na -- b [head=crowfoot-many tail=crowfoot-one]');
    const arrow = arrowOf(map);
    assert.deepEqual([arrow.startArrowhead, arrow.endArrowhead], ["crowfoot_one", "crowfoot_many"]);
    assert.match(read(map), /a <-> b \[head=crowfoot-many tail=crowfoot-one\]/);
    const again = apply(map, read(map));
    assert.deepEqual([again.updated, again.connected], [0, 0]);
  });

  test("re-routing a connection redraws it as the new kind", () => {
    const { map } = room();
    apply(map, 'a: server "A"\nb: server "B"\na -> b');
    apply(map, "a -> b [curved]");
    const arrow = arrowOf(map);
    assert.equal(arrow.elbowed, false);
    assert.deepEqual(arrow.roundness, { type: 2 });
    assert.equal((arrow.startBinding as { fixedPoint?: unknown }).fixedPoint, undefined);
  });

  test("an elbow a new node lands on is routed around it", () => {
    const { map } = room();
    apply(map, 'a: server "A"\nb: server "B"\na -> b');
    const before = JSON.stringify(arrowOf(map).points);
    // Placed beside a, which is where the existing arrow runs.
    apply(map, 'c: server "C"\na -> c');
    const scene = readScene(live(map));
    const c = scene.nodes.get("c")!.box;
    const ab = live(map).find((el) => el.type === "arrow" && (el.endBinding as { elementId: string }).elementId === scene.nodes.get("b")!.frameId)!;
    const pts = globalPoints(ab);
    for (let i = 1; i < pts.length; i++) {
      const mx = (pts[i - 1][0] + pts[i][0]) / 2;
      const my = (pts[i - 1][1] + pts[i][1]) / 2;
      assert.ok(!(mx > c.x && mx < c.x + c.width && my > c.y && my < c.y + c.height), `a -> b crosses c (was ${before})`);
    }
  });
});

// An interaction diagram says the same pair talks several times: browser to
// server, server back, browser again. Inside zones that made dagre throw
// "Not possible to find intersection inside of the rectangle", and the whole
// write failed with a 500.
describe("repeated connections between the same two nodes", () => {
  test("lay out, inside zones, and every one is drawn", () => {
    const { map } = room();
    const text = [
      'zone a "Browser" network {',
      '  spa: web-app "SPA"',
      "}",
      'zone b "Servers" trust {',
      '  core: server "Core"',
      '  db: database "DB"',
      "}",
      'spa -> core "1"',
      'core -> spa "2"',
      'spa -> core "3"',
      'core -> db "4"',
      'core -> db "5"',
      'spa -> core "6"',
    ].join("\n");
    const result = apply(map, text);
    assert.equal(result.connected, 6);
    assert.equal(readScene(live(map)).edges.length, 6);
  });
});
