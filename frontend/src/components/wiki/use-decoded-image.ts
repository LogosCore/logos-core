// The file card's side of wiki-image-decode.ts: decode a TIFF or HEIC once the
// card is about to scroll into view, the way loading="lazy" holds back the
// <img> of a format the browser draws itself.

import { useEffect, useState, type RefObject } from "react"

// Through the alias path, not "./": the viewer build replaces this module.
import { decodeImage } from "@/components/wiki/wiki-image-decode"
import type { DecodedImageFormat } from "./wiki-file-preview-image"

export interface DecodedImage {
  /** Object URL of the decoded picture, once it is ready. */
  src: string | null
  /** The decode failed or was declined; show the type icon instead. */
  failed: boolean
}

const IDLE: DecodedImage = { src: null, failed: false }

/** Starts this far before the card reaches the viewport, so the picture is
 *  usually there by the time it does. */
const ROOT_MARGIN = "300px"

export function useDecodedImage(
  format: DecodedImageFormat | null,
  url: string,
  size: number,
  targetRef: RefObject<HTMLElement | null>,
  eager: boolean,
): DecodedImage {
  const [state, setState] = useState<{ key: string; image: DecodedImage } | null>(null)
  const key = format && url ? `${format}:${url}` : ""

  useEffect(() => {
    if (!format || !url) return
    let cancelled = false
    const start = () => {
      decodeImage(url, format, size).then(
        (src) => !cancelled && setState({ key: `${format}:${url}`, image: { src, failed: false } }),
        () => !cancelled && setState({ key: `${format}:${url}`, image: { src: null, failed: true } }),
      )
    }
    const target = targetRef.current
    // The print page never scrolls, so waiting for the viewport would leave
    // a card below it without its picture.
    if (eager || !target || typeof IntersectionObserver === "undefined") {
      start()
      return () => {
        cancelled = true
      }
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((e) => e.isIntersecting)) return
        observer.disconnect()
        start()
      },
      { rootMargin: ROOT_MARGIN },
    )
    observer.observe(target)
    return () => {
      cancelled = true
      observer.disconnect()
    }
  }, [format, url, size, targetRef, eager])

  // A state left from an earlier file (the node's attrs changed) is not this
  // one's answer.
  return state && state.key === key ? state.image : IDLE
}
