// Mermaid, parsed on the server.
//
// Agents describe diagrams in Mermaid, which every model writes fluently.
// Excalidraw's own converter (@excalidraw/mermaid-to-excalidraw) renders the
// diagram in a browser to measure it, and there is no browser here. Mermaid's
// *parsers* need none: they turn the text into the diagram's structure
// (nodes, edges, subgraphs, messages), and the layout is ours
// (diagram-apply.ts for flowcharts, sequence-layout.ts for sequences).
//
// Two corners of Mermaid reach for a browser while parsing, and get a stub:
//
//   - DOMPurify sanitises label HTML. Labels become plain text in Excalidraw
//     shapes and are never rendered as HTML, so sanitize() passes text
//     through and hooks are ignored.
//   - A sequence diagram's `box` line checks whether its first word is a CSS
//     colour, through window.CSS.supports. A small colour test stands in.
//
// The window stub exists only while a parse runs, and parses run one at a
// time, so nothing else in this process ever sees a `window`.

import DOMPurify from "dompurify";

type MermaidAPI = {
  initialize(config: Record<string, unknown>): void;
  mermaidAPI: { getDiagramFromText(text: string): Promise<{ type: string; db: unknown }> };
};

let loading: Promise<MermaidAPI> | null = null;

function load(): Promise<MermaidAPI> {
  loading ??= (async () => {
    const purify = DOMPurify as unknown as Record<string, unknown>;
    purify.sanitize = (text: unknown) => text;
    purify.addHook = () => {};
    purify.removeHook = () => {};
    purify.removeAllHooks = () => {};
    const mermaid = (await import("mermaid")).default as unknown as MermaidAPI;
    mermaid.initialize({ startOnLoad: false, securityLevel: "loose" });
    return mermaid;
  })();
  return loading;
}

const CSS_COLOR_WORDS = new Set([
  "transparent", "black", "white", "gray", "grey", "silver", "red", "maroon", "orange", "yellow", "olive",
  "lime", "green", "teal", "aqua", "cyan", "blue", "navy", "purple", "fuchsia", "magenta", "pink",
  "lightblue", "lightgreen", "lightyellow", "lightgray", "lightgrey", "aliceblue", "lavender", "beige",
]);

const browserStub = {
  CSS: {
    supports: (_property: string, value: string) =>
      /^(?:rgba?|hsla?)\s*\(/i.test(value) || /^#[0-9a-f]{3,8}$/i.test(value) || CSS_COLOR_WORDS.has(value.toLowerCase()),
  },
};

let queue: Promise<unknown> = Promise.resolve();

/** Parse Mermaid text. Throws Mermaid's own error, which names the line. */
export async function parseMermaidText(text: string): Promise<{ type: string; db: unknown }> {
  const mermaid = await load();
  const run = async () => {
    const g = globalThis as Record<string, unknown>;
    const hadWindow = "window" in g;
    if (!hadWindow) g.window = browserStub;
    try {
      return await mermaid.mermaidAPI.getDiagramFromText(text);
    } finally {
      if (!hadWindow) delete g.window;
    }
  };
  const result = queue.then(run, run);
  queue = result.catch(() => undefined);
  return result;
}

/** Whether a text is Mermaid rather than the diagram language: its first
 * line that is not a comment names a Mermaid diagram type. */
export function isMermaid(text: string): boolean {
  const first = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line !== "" && !line.startsWith("%%") && !line.startsWith("#"));
  return !!first && /^(flowchart|graph|sequenceDiagram)\b/.test(first);
}
