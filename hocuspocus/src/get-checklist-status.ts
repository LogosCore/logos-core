// Read-only checklist status for a wiki document.
//
// Walks the Y.js document tree and returns per-item metadata without
// rendering the full markdown body. An agent uses this to see which
// checklist keys exist, which are answered, and how large each answer is,
// then decides what to fill without reading the entire page.

import type { Express, Request, Response } from "express";
import type { Hocuspocus } from "@hocuspocus/server";
import { XmlElement, XmlFragment, XmlText } from "yjs";
import {
  collectChecklistCoverage,
  isTruthyAttr,
  type ChecklistCoverage,
} from "./references.js";
import { WIKI_SCHEMA_VERSION } from "./wiki-schema-version.js";
import { readRawBody, requireSignature } from "./internal-auth.js";

const CHECKLIST_ITEM_NODE = "wikiChecklistItem";
const Y_FRAGMENT_FIELD = "default";

interface GetChecklistStatusRequest {
  documentId?: string;
}

interface ChecklistItemStatus {
  key: string;
  prompt: string;
  required: boolean;
  answered: boolean;
  state: string;
  answerBytes: number;
}

const ANSWER_BEARING_LEAF_NODES = new Set<string>([
  "wikiHostReference",
  "wikiCredentialReference",
  "wikiHashReference",
  "wikiFile",
  "image",
]);

function itemHasAnswer(node: XmlElement): boolean {
  for (const child of node.toArray()) {
    if (child instanceof XmlText) {
      if (child.toString().trim().length > 0) return true;
      continue;
    }
    if (!(child instanceof XmlElement)) continue;
    if (ANSWER_BEARING_LEAF_NODES.has(child.nodeName)) return true;
    if (itemHasAnswer(child)) return true;
  }
  return false;
}

function estimateContentBytes(node: XmlElement): number {
  let bytes = 0;
  for (const child of node.toArray()) {
    if (child instanceof XmlText) {
      bytes += Buffer.byteLength(child.toString(), "utf8");
      continue;
    }
    if (child instanceof XmlElement) {
      bytes += estimateContentBytes(child);
    }
  }
  return bytes;
}

type ItemState = "unanswered" | "answered" | "not_applicable" | "flagged";

function deriveItemState(item: XmlElement): ItemState {
  const explicit = item.getAttribute("state");
  if (explicit === "not_applicable") return "not_applicable";
  if (explicit === "flagged") return "flagged";
  return itemHasAnswer(item) ? "answered" : "unanswered";
}

function collectChecklistItems(
  node: XmlFragment | XmlElement,
  items: ChecklistItemStatus[],
): void {
  for (const child of node.toArray()) {
    if (!(child instanceof XmlElement)) continue;
    if (child.nodeName === CHECKLIST_ITEM_NODE) {
      const state = deriveItemState(child);
      items.push({
        key: String(child.getAttribute("key") ?? ""),
        prompt: String(child.getAttribute("prompt") ?? ""),
        required: isTruthyAttr(child.getAttribute("required")),
        answered: state === "answered" || state === "not_applicable",
        state,
        answerBytes: estimateContentBytes(child),
      });
      continue;
    }
    collectChecklistItems(child, items);
  }
}

function roomName(documentId: string): string {
  return `wiki/${documentId}`;
}

export function setupGetChecklistStatusApi(app: Express, server: Hocuspocus): void {
  app.post(
    "/internal/get-checklist-status",
    readRawBody(4096),
    async (req: Request, res: Response) => {
      const rawBody = requireSignature(req, res);
      if (!rawBody) return;

      let parsed: GetChecklistStatusRequest;
      try {
        parsed = JSON.parse(rawBody.toString("utf8")) as GetChecklistStatusRequest;
      } catch {
        res.status(400).json({ error: "malformed JSON" });
        return;
      }

      const { documentId } = parsed;
      if (typeof documentId !== "string" || documentId === "") {
        res.status(400).json({ error: "documentId field required" });
        return;
      }

      let connection;
      try {
        connection = await server.openDirectConnection(roomName(documentId), {
          agent: true,
          schemaVersion: WIKI_SCHEMA_VERSION,
        });

        let items: ChecklistItemStatus[] = [];
        let coverage: ChecklistCoverage = { total: 0, required: 0, answered: 0 };

        await connection.transact((document) => {
          const fragment = document.getXmlFragment(Y_FRAGMENT_FIELD);
          collectChecklistItems(fragment, items);
          coverage = collectChecklistCoverage(fragment);
        });

        res.status(200).json({ items, coverage });
      } catch (err) {
        const message = err instanceof Error ? err.message : "get-checklist-status failed";
        console.error("get-checklist-status error:", err);
        res.status(500).json({ error: message });
      } finally {
        if (connection) {
          try {
            await connection.disconnect();
          } catch (err) {
            console.warn("get-checklist-status: failed to close direct connection:", err);
          }
        }
      }
    },
  );
}
