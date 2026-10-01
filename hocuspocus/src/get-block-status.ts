// Read-only keyed-block status for a wiki document.
//
// Walks the Y.js document tree and returns per-block metadata for keyed
// block nodes (currently wikiNotice). An agent uses this to discover which
// notice keys exist and whether each block has content, then writes via
// set_block_content.
//
// Mirrors get-checklist-status for notices.

import type { Express, Request, Response } from "express";
import type { Hocuspocus } from "@hocuspocus/server";
import { XmlElement, XmlFragment, XmlText } from "yjs";
import { WIKI_SCHEMA_VERSION } from "./wiki-schema-version.js";
import { readRawBody, requireSignature } from "./internal-auth.js";

const Y_FRAGMENT_FIELD = "default";

const KEYED_BLOCK_NODES = new Set(["wikiNotice"]);

interface GetBlockStatusRequest {
  documentId?: string;
}

interface BlockStatus {
  key: string;
  nodeType: string;
  variant: string;
  hasContent: boolean;
  contentBytes: number;
}

function blockHasContent(node: XmlElement): boolean {
  for (const child of node.toArray()) {
    if (child instanceof XmlText) {
      if (child.toString().trim().length > 0) return true;
      continue;
    }
    if (!(child instanceof XmlElement)) continue;
    if (blockHasContent(child)) return true;
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

function collectKeyedBlocks(
  node: XmlFragment | XmlElement,
  blocks: BlockStatus[],
): void {
  for (const child of node.toArray()) {
    if (!(child instanceof XmlElement)) continue;
    if (KEYED_BLOCK_NODES.has(child.nodeName)) {
      const key = child.getAttribute("key");
      if (typeof key === "string" && key.length > 0) {
        blocks.push({
          key,
          nodeType: child.nodeName,
          variant: String(child.getAttribute("variant") ?? ""),
          hasContent: blockHasContent(child),
          contentBytes: estimateContentBytes(child),
        });
      }
      continue;
    }
    collectKeyedBlocks(child, blocks);
  }
}

function roomName(documentId: string): string {
  return `wiki/${documentId}`;
}

export function setupGetBlockStatusApi(app: Express, server: Hocuspocus): void {
  app.post(
    "/internal/get-block-status",
    readRawBody(4096),
    async (req: Request, res: Response) => {
      const rawBody = requireSignature(req, res);
      if (!rawBody) return;

      let parsed: GetBlockStatusRequest;
      try {
        parsed = JSON.parse(rawBody.toString("utf8")) as GetBlockStatusRequest;
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

        const blocks: BlockStatus[] = [];

        await connection.transact((document) => {
          const fragment = document.getXmlFragment(Y_FRAGMENT_FIELD);
          collectKeyedBlocks(fragment, blocks);
        });

        res.status(200).json({ blocks, total: blocks.length });
      } catch (err) {
        const message = err instanceof Error ? err.message : "get-block-status failed";
        console.error("get-block-status error:", err);
        res.status(500).json({ error: message });
      } finally {
        if (connection) {
          try {
            await connection.disconnect();
          } catch (err) {
            console.warn("get-block-status: failed to close direct connection:", err);
          }
        }
      }
    },
  );
}
