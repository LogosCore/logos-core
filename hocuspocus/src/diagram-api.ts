// Internal routes for reading and writing a drawing as a diagram
// (diagram-language.ts). Same room seat and raw-body HMAC as apply-drawing.ts:
// a write is an ordinary Y.js transaction in the live room, so it merges with
// whoever is drawing and persists through the normal store.
//
//   POST /internal/drawing/diagram/read
//     Request  { documentId }
//     Response { diagram, nodes, edges, zones, others: [{id,type,text?,x,y}],
//                othersTotal, watchers }
//
//   POST /internal/drawing/diagram/apply
//     Request  { documentId, diagram, mode?: "merge"|"replace", userId? }
//     Response { ok, created, updated, removed, connected, warnings,
//                diagram, watchers }
//     422      { error, problems: [{line, message}] }
//
// The apply answers with the drawing as it now reads, so the caller sees the
// result of its write, ids included, without a second call.

import type { Express, Request, Response } from "express";
import type { Hocuspocus } from "@hocuspocus/server";
import type * as Y from "yjs";

import { readRawBody, requireSignature } from "./internal-auth.js";
import { elementsOf, release, roomName, watcherCount } from "./apply-drawing.js";
import { DiagramError, parseDiagram, type Diagram } from "./diagram-language.js";
import { isMermaid } from "./mermaid-parse.js";
import { aliasesOf, flowchartToDiagram, writeMermaid } from "./mermaid-flowchart.js";
import { drawSequence, parseSequence } from "./sequence-layout.js";
import { applyDiagram, type DiagramMode } from "./diagram-apply.js";
import { readScene, sceneToText, type Scene } from "./diagram-read.js";
import type { NormalizedElement } from "./drawing-elements.js";

const MAX_BODY_BYTES = 1024 * 1024;
/** Loose shapes listed by id; past this they are counted. */
const MAX_OTHERS = 60;

function liveElements(document: Y.Doc): NormalizedElement[] {
  return [...elementsOf(document).values()].filter((el) => el && !el.isDeleted);
}

function describe(scene: Scene) {
  const hasGraph = scene.nodes.size > 0 || scene.zones.size > 0;
  return {
    // The drawing's nodes, zones and connections as a Mermaid flowchart, and
    // each sequence diagram as the Mermaid it was drawn from.
    mermaid: hasGraph ? writeMermaid(scene) : "",
    sequences: [...scene.sequences.values()].map((s) => ({ id: s.id, mermaid: s.source })),
    // The same graph in the older diagram language.
    diagram: sceneToText(scene),
    nodes: scene.nodes.size,
    edges: scene.edges.length,
    zones: scene.zones.size,
    others: scene.others.slice(0, MAX_OTHERS),
    othersTotal: scene.others.length,
  };
}

export function setupDiagramApi(app: Express, server: Hocuspocus): void {
  app.post("/internal/drawing/diagram/read", readRawBody(MAX_BODY_BYTES), async (req: Request, res: Response) => {
    const rawBody = requireSignature(req, res);
    if (!rawBody) return;
    let body: { documentId?: unknown };
    try {
      body = JSON.parse(rawBody.toString("utf8"));
    } catch {
      res.status(400).json({ error: "malformed JSON" });
      return;
    }
    if (typeof body.documentId !== "string" || body.documentId === "") {
      res.status(400).json({ error: "documentId field required" });
      return;
    }
    const documentId = body.documentId;

    let connection;
    try {
      connection = await server.openDirectConnection(roomName(documentId), { agent: true });
      // Read from the room's document directly, not inside transact(): a
      // direct connection's transact always runs the store hook, and a read
      // has nothing to store.
      const document = (connection as unknown as { document: Y.Doc | null }).document;
      if (!document) throw new Error("the drawing's room did not open");
      res.status(200).json({ ...describe(readScene(liveElements(document))), watchers: watcherCount(server, documentId) });
    } catch (err) {
      console.error("diagram read error:", err);
      res.status(500).json({ error: err instanceof Error ? err.message : "read failed" });
    } finally {
      await release(connection, "diagram read");
    }
  });

  app.post("/internal/drawing/diagram/apply", readRawBody(MAX_BODY_BYTES), async (req: Request, res: Response) => {
    const rawBody = requireSignature(req, res);
    if (!rawBody) return;
    let body: { documentId?: unknown; diagram?: unknown; mode?: unknown; userId?: unknown };
    try {
      body = JSON.parse(rawBody.toString("utf8"));
    } catch {
      res.status(400).json({ error: "malformed JSON" });
      return;
    }
    if (typeof body.documentId !== "string" || body.documentId === "") {
      res.status(400).json({ error: "documentId field required" });
      return;
    }
    if (typeof body.diagram !== "string" || body.diagram.trim() === "") {
      res.status(400).json({ error: "diagram field required" });
      return;
    }
    const documentId = body.documentId;
    const mode: DiagramMode = body.mode === "replace" ? "replace" : "merge";

    // Parsed before a seat is taken: a text with problems never touches the
    // room.
    const text = body.diagram;
    const mermaid = isMermaid(text);
    const sequence = mermaid && /^\s*(?:%%[^\n]*\n\s*)*sequenceDiagram\b/.test(text);

    // Everything that can be checked without the room is checked before a
    // seat is taken: a text with problems never touches it.
    let parsed: Awaited<ReturnType<typeof parseSequence>> | null = null;
    let diagram: Diagram | null = null;
    const warnings: string[] = [];
    try {
      if (sequence) parsed = await parseSequence(text);
      else if (!mermaid) diagram = parseDiagram(text);
    } catch (err) {
      if (err instanceof DiagramError) {
        res.status(422).json({ error: err.message, problems: err.problems });
        return;
      }
      throw err;
    }

    let connection;
    try {
      connection = await server.openDirectConnection(roomName(documentId), {
        agent: true,
        // Read back in persistence.ts, which attributes the save.
        userId: body.userId,
      });
      // A flowchart needs the drawing first: a read named the nodes that have
      // no id of their own by aliases, and those map back to element ids.
      if (mermaid && !sequence) {
        const room = (connection as unknown as { document: Y.Doc | null }).document;
        const before = room ? readScene(liveElements(room)) : readScene([]);
        try {
          const converted = await flowchartToDiagram(text, aliasesOf(before));
          diagram = converted.diagram;
          warnings.push(...converted.warnings);
        } catch (err) {
          if (err instanceof DiagramError) {
            res.status(422).json({ error: err.message, problems: err.problems });
            return;
          }
          throw err;
        }
      }

      let result: Record<string, unknown> = {};
      let after: Scene | null = null;
      try {
        await connection.transact((document) => {
          // One Y.js transaction, so everyone in the room receives the
          // finished change as one update. A direct connection's transact()
          // does not open one, and every write in it went out on its own:
          // an open canvas saw arrows before they were routed and could
          // write those copies back over the routed ones.
          document.transact(() => {
            const elements = elementsOf(document);
            if (parsed) {
              if (mode === "replace") {
                for (const el of liveElements(document)) {
                  elements.set(el.id, { ...el, isDeleted: true, version: (el.version ?? 1) + 1, updated: Date.now() });
                }
              }
              const drawn = drawSequence(elements, readScene(liveElements(document)), text, parsed);
              result = { kind: "sequence", sequence: drawn.id, participants: drawn.participants, messages: drawn.messages, warnings: drawn.warnings };
            } else {
              const applied = applyDiagram(elements, diagram as Diagram, mode);
              result = { kind: mermaid ? "flowchart" : "diagram", ...applied, warnings: [...warnings, ...applied.warnings] };
            }
          });
          after = readScene(liveElements(document));
        });
      } catch (err) {
        // References are checked against the drawing inside the
        // transaction, before anything is written; a problem there throws
        // out of it with nothing applied.
        if (err instanceof DiagramError) {
          res.status(422).json({ error: err.message, problems: err.problems });
          return;
        }
        throw err;
      }
      res.status(200).json({
        ok: true,
        ...result,
        ...describe(after as unknown as Scene),
        watchers: watcherCount(server, documentId),
      });
    } catch (err) {
      console.error("diagram apply error:", err);
      res.status(500).json({ error: err instanceof Error ? err.message : "apply failed" });
    } finally {
      await release(connection, "diagram apply");
    }
  });
}
