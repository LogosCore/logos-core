// Answers the file card's preview fetch from the bundle's data.
//
// The app's file card previews text, office and HTML attachments by
// fetch()ing their bytes, and a file:// page may not fetch — the browser
// refuses with a NetworkError. The exporter writes each such file's bytes as
// data/files/<id>.js, which calls window.__logosFile(id, base64) when loaded;
// this hands a fetch for files/<id>/… the bytes from there, so the card's own
// preview code runs unchanged. Every other fetch goes through untouched.

declare global {
  interface Window {
    __logosFiles?: Record<string, string>
  }
}

const FILE_URL = /(?:^|\/)files\/([0-9a-fA-F-]{36})\/[^?#]*(?:[?#].*)?$/

const pending = new Map<string, Promise<string>>()

function loadFileData(id: string): Promise<string> {
  const loaded = window.__logosFiles?.[id]
  if (loaded !== undefined) return Promise.resolve(loaded)
  let p = pending.get(id)
  if (!p) {
    p = new Promise<string>((resolve, reject) => {
      const script = document.createElement("script")
      script.src = `data/files/${id}.js`
      script.async = true
      script.onload = () => {
        const data = window.__logosFiles?.[id]
        if (data !== undefined) resolve(data)
        else reject(new TypeError("This file's preview data is empty."))
      }
      script.onerror = () => {
        pending.delete(id)
        reject(new TypeError("This file has no preview in the export. Use the download link."))
      }
      document.head.appendChild(script)
    })
    pending.set(id, p)
  }
  return p
}

function decode(base64: string): Uint8Array<ArrayBuffer> {
  const bin = atob(base64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export function installFileFetch() {
  const realFetch = window.fetch.bind(window)
  window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    const m = FILE_URL.exec(url)
    if (!m) return realFetch(input, init)
    return loadFileData(m[1]).then((b64) => new Response(decode(b64), { status: 200 }))
  }
}
