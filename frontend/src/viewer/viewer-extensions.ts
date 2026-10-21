import { Node, type AnyExtension } from "@tiptap/core"
import { ReactNodeViewRenderer, type ReactNodeViewProps } from "@tiptap/react"
import type { ComponentType } from "react"
import { wikiContentExtensions } from "@/components/wiki/wiki-content-extensions"
import {
  StaticCredentialChip,
  StaticDocumentChip,
  StaticHashChip,
  StaticHostChip,
} from "./viewer-chips"

const STATIC_CHIPS: Record<string, ComponentType<ReactNodeViewProps>> = {
  wikiDocumentReference: StaticDocumentChip,
  wikiHostReference: StaticHostChip,
  wikiHashReference: StaticHashChip,
  wikiCredentialReference: StaticCredentialChip,
}

/**
 * The editor's own content extensions, with the four reference chips swapped
 * for static views of what the exporter resolved. Each chip also declares the
 * `export` attribute the data carries: ProseMirror drops attributes a node
 * type does not declare, so without it the resolved values never reach the
 * view.
 */
export function viewerExtensions(): AnyExtension[] {
  return wikiContentExtensions().map((ext) => {
    const view = STATIC_CHIPS[ext.name]
    if (!view || !(ext instanceof Node)) return ext
    return ext.extend({
      addAttributes() {
        return {
          ...this.parent?.(),
          export: { default: null, rendered: false },
        }
      },
      addNodeView() {
        return ReactNodeViewRenderer(view)
      },
    })
  })
}
