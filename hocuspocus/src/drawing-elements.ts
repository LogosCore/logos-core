// Turn what an agent sends into a real Excalidraw element.
//
// The agent-facing contract is Excalidraw's own element shape — no invented
// DSL, no diagram language to learn or document. But a genuine element carries
// about twenty-five fields, most of which are bookkeeping (`seed`,
// `versionNonce`, `groupIds`, `roundness`) that nobody composing a diagram
// should have to think about and that a model will get wrong or omit. So every
// field but `type` is optional here, and the defaults are filled in.
//
// Validation is strict about the things that cannot be recovered from — an
// unknown type, a non-finite coordinate — and lenient about everything else. A
// rejected call costs the agent a round trip and tells it what to fix; a
// silently-accepted bad element becomes an invisible shape on somebody's
// canvas that neither of them can explain.

const SHAPE_TYPES = new Set([
  "rectangle",
  "ellipse",
  "diamond",
  "text",
  "arrow",
  "line",
  "freedraw",
  "image",
  "frame",
]);

/** Excalidraw's own defaults, so an agent-drawn shape looks like a hand-drawn
 * one rather than announcing itself as machine-made. */
const DEFAULTS = {
  strokeColor: "#1e1e1e",
  backgroundColor: "transparent",
  fillStyle: "solid",
  strokeWidth: 2,
  strokeStyle: "solid",
  roughness: 1,
  opacity: 100,
  fontSize: 20,
  // 5 is Excalifont, Excalidraw's hand-drawn face and its own default.
  fontFamily: 5,
  textAlign: "left",
  verticalAlign: "top",
} as const;

export interface NormalizedElement {
  [key: string]: unknown;
  id: string;
  type: string;
  version: number;
  versionNonce: number;
  isDeleted: boolean;
}

/** One end of an arrow, once normalized. */
interface ElementBinding {
  elementId: string;
  focus: number;
  gap: number;
}

/**
 * Accept a binding as either Excalidraw's own object or the bare id of the
 * shape to bind to, and fill the rest.
 *
 * `focus` and `gap` are geometry Excalidraw recomputes as soon as anything
 * moves; requiring a caller to invent them is asking for numbers that are
 * wrong the moment they are used. 0 means "aim at the centre", which is what
 * dragging an arrow onto a shape produces.
 */
function binding(value: unknown): ElementBinding | null {
  if (typeof value === "string" && value !== "") {
    return { elementId: value, focus: 0, gap: 4 };
  }
  if (typeof value === "object" && value !== null) {
    const v = value as Record<string, unknown>;
    if (typeof v.elementId === "string" && v.elementId !== "") {
      return {
        elementId: v.elementId,
        focus: typeof v.focus === "number" ? v.focus : 0,
        gap: typeof v.gap === "number" ? v.gap : 4,
      };
    }
  }
  return null;
}

export class DrawingElementError extends Error {}

// Text measurement, for sizing a box to what it holds.
// ---------------------------------------------------------------------------
// A shape whose width was left to the default held a label that did not fit:
// `{"type":"rectangle","label":"Domain Controller"}` produced a 100px box
// around roughly 130px of text, the words ran over the border, and nothing
// said so. Free-standing text had the same problem from the same default.
//
// Exact measurement needs the font and a renderer, neither of which exists
// here. This is a deliberate approximation, and it errs wide: a box slightly
// too large is untidy, a box too small clips the words. Anyone who wants exact
// geometry can still send explicit width and height, which always win.

const LABEL_PAD_X = 16;
const LABEL_PAD_Y = 12;
const LINE_HEIGHT = 1.25;
/** Where a long label starts wrapping instead of growing a very wide box. */
const LABEL_MAX_TEXT_WIDTH = 320;
const MIN_LABELLED_WIDTH = 100;
const MIN_LABELLED_HEIGHT = 48;

/**
 * Whether a character occupies a full em rather than roughly half of one.
 *
 * CJK, Hangul, fullwidth forms and emoji are about twice the width of Latin at
 * the same font size. Cyrillic and Greek are not — they measure like Latin, so
 * they are deliberately absent. Getting this wrong truncates labels in exactly
 * the scripts nobody tests with.
 */
function isWideChar(cp: number): boolean {
  return (
    (cp >= 0x1100 && cp <= 0x115f) ||
    (cp >= 0x2e80 && cp <= 0xa4cf) ||
    (cp >= 0xac00 && cp <= 0xd7a3) ||
    (cp >= 0xf900 && cp <= 0xfaff) ||
    (cp >= 0xfe30 && cp <= 0xfe6f) ||
    (cp >= 0xff00 && cp <= 0xff60) ||
    (cp >= 0xffe0 && cp <= 0xffe6) ||
    (cp >= 0x1f300 && cp <= 0x1faff)
  );
}

/** Advance width of one line, in pixels. 0.6em is a reasonable mean for the
 * proportional faces Excalidraw ships. */
function lineWidth(line: string, fontSize: number): number {
  let width = 0;
  for (const ch of line) {
    const cp = ch.codePointAt(0) ?? 0;
    width += isWideChar(cp) ? fontSize : fontSize * 0.6;
  }
  return width;
}

/**
 * Break a string into lines no wider than maxWidth, greedily at spaces, and
 * keep the line breaks it already has. A single word wider than the line is
 * left whole rather than split mid-word.
 *
 * Bound text has to be stored wrapped. Excalidraw draws a label's `text`
 * exactly as stored and only re-wraps it from `originalText` when somebody
 * edits or resizes the shape, so a long label written as one line ran out
 * of its note on one line until then.
 */
export function wrapText(text: string, fontSize: number, maxWidth = LABEL_MAX_TEXT_WIDTH): string {
  const lines: string[] = [];
  for (const paragraph of text.split("\n")) {
    const words = paragraph.split(/\s+/).filter(Boolean);
    let current = "";
    for (const word of words) {
      const candidate = current === "" ? word : `${current} ${word}`;
      if (current !== "" && lineWidth(candidate, fontSize) > maxWidth) {
        lines.push(current);
        current = word;
      } else {
        current = candidate;
      }
    }
    lines.push(current);
  }
  return lines.join("\n");
}

/** Measure a string as wrapText lays it out. */
export function measureText(
  text: string,
  fontSize: number,
  maxWidth = LABEL_MAX_TEXT_WIDTH,
): { width: number; height: number } {
  const lines = wrapText(text, fontSize, maxWidth).split("\n");
  const widest = Math.max(0, ...lines.map((line) => lineWidth(line, fontSize)));
  return { width: widest, height: Math.max(1, lines.length) * fontSize * LINE_HEIGHT };
}

/** The fields of a bound label holding `text`, wrapped to fit inside a
 * container `containerWidth` wide. */
export function boundTextFields(
  text: string,
  fontSize: number,
  containerWidth: number,
): { text: string; originalText: string; width: number; height: number } {
  const maxWidth = Math.max(containerWidth - 2 * BOUND_TEXT_PAD, 16);
  const measured = measureText(text, fontSize, maxWidth);
  return {
    text: wrapText(text, fontSize, maxWidth),
    originalText: text,
    width: Math.min(Math.ceil(measured.width), maxWidth),
    height: Math.ceil(measured.height),
  };
}

/** Space between a bound label and its container's sides. */
const BOUND_TEXT_PAD = 8;

function randomInteger(): number {
  return Math.floor(Math.random() * 2 ** 31);
}

export function randomID(): string {
  // Excalidraw ids are opaque; any stable unique string works. Hex keeps them
  // recognisable alongside the ones it mints itself.
  return Array.from({ length: 5 }, () =>
    randomInteger().toString(16).padStart(8, "0"),
  )
    .join("")
    .slice(0, 36);
}

function num(value: unknown, fallback: number, field: string): number {
  if (value === undefined || value === null) return fallback;
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new DrawingElementError(
      `${field} must be a finite number, got ${JSON.stringify(value)}`,
    );
  }
  return value;
}

/**
 * Fill an agent-supplied element out into one Excalidraw will render.
 *
 * `seed` and `versionNonce` are randomised rather than fixed. Excalidraw uses
 * the seed to generate a shape's hand-drawn jitter, so a constant would make
 * every rectangle an agent drew identically wobbly — visibly a template. The
 * nonce is the tiebreak when two clients edit the same element at the same
 * version, and a constant would make that tiebreak decide by nothing.
 */
export function normalizeElement(input: unknown): NormalizedElement {
  if (typeof input !== "object" || input === null || Array.isArray(input)) {
    throw new DrawingElementError("each element must be an object");
  }
  const el = input as Record<string, unknown>;

  const type = el.type;
  if (typeof type !== "string" || !SHAPE_TYPES.has(type)) {
    throw new DrawingElementError(
      `unknown element type ${JSON.stringify(type)}; supported: ${[...SHAPE_TYPES].join(", ")}`,
    );
  }

  // A text element's box comes from its words unless the caller sized it.
  // The old 100x100 default clipped anything longer than a short word.
  //
  // Measured as stored: free text keeps its own line breaks and is never
  // wrapped (textFields), so wrapping it here at the label width gave a long
  // one-line title the box of two short lines — half as wide and twice as
  // tall as what is drawn.
  let defaultWidth = 100;
  let defaultHeight = 100;
  if (type === "text" && typeof el.text === "string" && el.text !== "") {
    const measured = measureText(el.text, num(el.fontSize, DEFAULTS.fontSize, "fontSize"), Infinity);
    defaultWidth = Math.ceil(measured.width);
    defaultHeight = Math.ceil(measured.height);
  }

  const base: NormalizedElement = {
    id: typeof el.id === "string" && el.id !== "" ? el.id : randomID(),
    type,
    x: num(el.x, 0, "x"),
    y: num(el.y, 0, "y"),
    width: num(el.width, defaultWidth, "width"),
    height: num(el.height, defaultHeight, "height"),
    angle: num(el.angle, 0, "angle"),
    strokeColor: el.strokeColor ?? DEFAULTS.strokeColor,
    backgroundColor: el.backgroundColor ?? DEFAULTS.backgroundColor,
    fillStyle: el.fillStyle ?? DEFAULTS.fillStyle,
    strokeWidth: num(el.strokeWidth, DEFAULTS.strokeWidth, "strokeWidth"),
    strokeStyle: el.strokeStyle ?? DEFAULTS.strokeStyle,
    roughness: num(el.roughness, DEFAULTS.roughness, "roughness"),
    opacity: num(el.opacity, DEFAULTS.opacity, "opacity"),
    groupIds: Array.isArray(el.groupIds) ? el.groupIds : [],
    frameId: el.frameId ?? null,
    roundness: el.roundness ?? null,
    seed: num(el.seed, randomInteger(), "seed"),
    version: num(el.version, 1, "version"),
    versionNonce: num(el.versionNonce, randomInteger(), "versionNonce"),
    isDeleted: el.isDeleted === true,
    boundElements: el.boundElements ?? null,
    updated: num(el.updated, Date.now(), "updated"),
    link: el.link ?? null,
    locked: el.locked === true,
    // Excalidraw's own fractional index. Left as it arrived — it is the app's
    // to maintain, and it is what orders shapes a person drew. Agent-written
    // elements have none, which is what `z` below is for.
    index: el.index ?? null,
    // Layer. Our own field, because an agent needs an integer it can reason
    // about ("put the arrows under the boxes") where Excalidraw's index is an
    // opaque fractional key. Absent here and assigned on write, so that a
    // shape with no opinion lands on top of what is already there.
    ...(typeof el.z === "number" && Number.isFinite(el.z) ? { z: el.z } : {}),
    // Excalidraw's field for host data, kept through every edit and copy. The
    // built-in shapes tag their parts here (drawing-catalogue.ts), which is how
    // a read tells a card from loose lines and text.
    ...(typeof el.customData === "object" && el.customData !== null ? { customData: el.customData } : {}),
  };

  switch (type) {
    case "text":
      return { ...base, ...textFields(el) };
    case "arrow":
    case "line": {
      const linear = linearFields(el);
      // A line's box is the extent of its points, which is what Excalidraw
      // draws from. Left at the 100x100 default, a flat 300px arrow claimed
      // to be 100 wide and 100 tall — and an arrow's label is wrapped to its
      // width.
      const pts = linear.points as number[][];
      if (pts.length >= 2 && pts.every((p) => Array.isArray(p) && Number.isFinite(p[0]) && Number.isFinite(p[1]))) {
        const xs = pts.map((p) => p[0]);
        const ys = pts.map((p) => p[1]);
        base.width = Math.max(...xs) - Math.min(...xs);
        base.height = Math.max(...ys) - Math.min(...ys);
      }
      return { ...base, ...linear };
    }
    case "freedraw":
      return {
        ...base,
        points: Array.isArray(el.points) ? el.points : [],
        pressures: Array.isArray(el.pressures) ? el.pressures : [],
        simulatePressure: el.simulatePressure !== false,
        lastCommittedPoint: el.lastCommittedPoint ?? null,
      };
    case "image":
      return {
        ...base,
        fileId: el.fileId ?? null,
        scale: Array.isArray(el.scale) ? el.scale : [1, 1],
        status: el.status ?? "saved",
        crop: el.crop ?? null,
      };
    case "frame":
      return { ...base, name: typeof el.name === "string" ? el.name : null };
    default:
      return base;
  }
}

function textFields(el: Record<string, unknown>): Record<string, unknown> {
  const text = typeof el.text === "string" ? el.text : "";
  if (text === "") {
    throw new DrawingElementError("a text element needs a non-empty text");
  }
  return {
    text,
    // originalText is what Excalidraw re-wraps from when a bound label's
    // container is resized. Diverging from `text` makes a label silently
    // revert to different words the first time somebody drags its box.
    originalText: typeof el.originalText === "string" ? el.originalText : text,
    fontSize: num(el.fontSize, DEFAULTS.fontSize, "fontSize"),
    fontFamily: num(el.fontFamily, DEFAULTS.fontFamily, "fontFamily"),
    textAlign: el.textAlign ?? DEFAULTS.textAlign,
    verticalAlign: el.verticalAlign ?? DEFAULTS.verticalAlign,
    containerId: el.containerId ?? null,
    lineHeight: num(el.lineHeight, 1.25, "lineHeight"),
    autoResize: el.autoResize !== false,
  };
}

function linearFields(el: Record<string, unknown>): Record<string, unknown> {
  // Excalidraw draws a linear element from its points, not its width/height —
  // an arrow with no points renders as nothing at all. Default to a straight
  // run across the element's own box so a caller that gave only geometry still
  // gets a visible line.
  // Points decide where a line is drawn; width and height do not. When the
  // caller gave neither points nor a binding, a straight run across the
  // element's own box is the only sensible guess.
  //
  // When it DID give a binding, the points are left empty for the apply step
  // to derive from the shapes being connected — it has the scene and this does
  // not. Guessing from width/height there is how eighteen arrows bound to
  // eighteen different boxes were all drawn as the same stroke: the bindings
  // were right, the geometry was identical, and the result said applied: 18.
  const bound = el.startBinding !== undefined || el.endBinding !== undefined;
  const points = Array.isArray(el.points) && el.points.length >= 2
    ? el.points
    : bound
      ? []
      : [
          [0, 0],
          [num(el.width, 100, "width"), num(el.height, 0, "height")],
        ];

  return {
    points,
    lastCommittedPoint: el.lastCommittedPoint ?? null,
    startBinding: binding(el.startBinding),
    endBinding: binding(el.endBinding),
    startArrowhead: el.startArrowhead ?? null,
    endArrowhead: el.endArrowhead ?? (el.type === "arrow" ? "arrow" : null),
    elbowed: el.elbowed === true,
  };
}

/**
 * Grow a shape so its label fits, leaving any dimension the caller set alone.
 *
 * Only ever grows. A caller who asked for a 400px box and gave it one word
 * meant the 400px; a caller who gave no width meant "whatever it takes".
 */
function sizeToLabel(
  el: NormalizedElement,
  label: string,
  widthGiven: boolean,
  heightGiven: boolean,
): NormalizedElement {
  const measured = measureText(label, 16);
  const next = { ...el };

  if (!widthGiven) {
    next.width = Math.max(MIN_LABELLED_WIDTH, Math.ceil(measured.width) + LABEL_PAD_X * 2);
  }
  if (!heightGiven) {
    next.height = Math.max(MIN_LABELLED_HEIGHT, Math.ceil(measured.height) + LABEL_PAD_Y * 2);
  }
  return next;
}

/**
 * Build the text element that sits inside a shape, for the `label` shorthand.
 *
 * Excalidraw models a labelled box as two elements — the shape, and a text
 * whose containerId points at it — and the pair only holds together if the
 * shape *also* lists the text in its boundElements. Half-wiring it looks
 * correct until somebody drags the box and the words stay behind. That is
 * bookkeeping, not composition, so it is done here rather than asked for.
 */
function labelFor(container: NormalizedElement, text: string): NormalizedElement {
  const fontSize = 16;
  const width = Number(container.width) || 100;
  const height = Number(container.height) || 100;
  const fields = boundTextFields(text, fontSize, width);
  const lines = fields.text.split("\n").length;

  return normalizeElement({
    type: "text",
    id: `${container.id}-label`,
    text: fields.text,
    originalText: fields.originalText,
    fontSize,
    containerId: container.id,
    textAlign: "center",
    verticalAlign: "middle",
    strokeColor: container.strokeColor,
    // Excalidraw re-lays a bound label out on load; these are a sane starting
    // box so nothing jumps on first render.
    x: Number(container.x) + 8,
    y: Number(container.y) + height / 2 - (fontSize * 1.25 * lines) / 2,
    width: Math.max(width - 16, 16),
    height: fontSize * 1.25 * lines,
  });
}

/** The shapes Excalidraw will bind a label to. Anything else given a label
 * produced a text pointing at a container that cannot hold it, which
 * Excalidraw unbinds on load and leaves loose wherever it was written. */
const LABEL_CONTAINERS = new Set(["rectangle", "ellipse", "diamond", "arrow"]);

const ARROW_LABEL_FONT_SIZE = 16;

/** Why a shape cannot carry `label`, or null when it can. */
function labelRefusal(type: string): string | null {
  if (LABEL_CONTAINERS.has(type)) return null;
  if (type === "line") return "a line cannot hold a label — use an arrow with endArrowhead: null, which can";
  if (type === "frame") return "a frame is titled by `name`, not `label`";
  return `a ${type} cannot hold a label — place a text element beside it`;
}

/**
 * How wide an arrow's label may be before it wraps: Excalidraw's own rule
 * (getBoundTextMaxWidth), so the lines stored here are the lines it draws.
 */
function arrowLabelMaxWidth(arrow: NormalizedElement, fontSize: number): number {
  return Math.max(0.7 * Math.abs(Number(arrow.width) || 0), fontSize * 11);
}

/**
 * The middle of an arrow, where Excalidraw centres its label: the middle
 * point of an odd number of points, else the middle of the middle segment.
 * Taken straight even on a curved arrow, which is close enough for a
 * starting box — Excalidraw places the label itself when it draws.
 */
function arrowMidpoint(arrow: NormalizedElement): { x: number; y: number } | null {
  const points = arrow.points;
  if (!Array.isArray(points) || points.length < 2) return null;
  const ox = Number(arrow.x) || 0;
  const oy = Number(arrow.y) || 0;
  const at = (i: number) => (points as number[][])[i];
  if (points.length % 2 === 1) {
    const [px, py] = at(Math.floor(points.length / 2));
    return { x: ox + px, y: oy + py };
  }
  const i = points.length / 2 - 1;
  const [ax, ay] = at(i);
  const [bx, by] = at(i + 1);
  return { x: ox + (ax + bx) / 2, y: oy + (ay + by) / 2 };
}

/**
 * Fit an arrow's label to the arrow as it now stands: wrapped from its own
 * words to the width Excalidraw allows, and centred on the arrow's middle.
 *
 * Needed whenever the arrow's geometry is settled or changes, which for a
 * bound arrow is only once the server has placed it (apply-drawing.ts), so
 * this is run again there.
 */
export function fitArrowLabel(arrow: NormalizedElement, text: NormalizedElement): NormalizedElement {
  const fontSize = Number(text.fontSize) || ARROW_LABEL_FONT_SIZE;
  const words = typeof text.originalText === "string" ? text.originalText : String(text.text ?? "");
  const maxWidth = arrowLabelMaxWidth(arrow, fontSize);
  const wrapped = wrapText(words, fontSize, maxWidth);
  const measured = measureText(words, fontSize, maxWidth);
  const width = Math.min(Math.ceil(measured.width), Math.ceil(maxWidth));
  const height = Math.ceil(measured.height);
  const mid = arrowMidpoint(arrow);
  return {
    ...text,
    text: wrapped,
    originalText: words,
    width,
    height,
    ...(mid ? { x: mid.x - width / 2, y: mid.y - height / 2 } : {}),
  };
}

/**
 * The label for a shape or an arrow, wired to it from the text's side. The
 * caller adds the text to the container's boundElements.
 */
export function labelElementFor(container: NormalizedElement, words: string): NormalizedElement {
  if (container.type !== "arrow") return labelFor(container, words);
  const text = normalizeElement({
    type: "text",
    id: `${container.id}-label`,
    text: words,
    originalText: words,
    fontSize: ARROW_LABEL_FONT_SIZE,
    containerId: container.id,
    textAlign: "center",
    verticalAlign: "middle",
    strokeColor: container.strokeColor,
    x: Number(container.x) || 0,
    y: Number(container.y) || 0,
  });
  return fitArrowLabel(container, text);
}

/** Whether `label` may be given to a shape of this type. */
export function canHoldLabel(type: unknown): boolean {
  return typeof type === "string" && LABEL_CONTAINERS.has(type);
}

/**
 * Normalize a batch, expanding the `label` shorthand and naming which entry
 * failed — an agent told only "invalid element" has to bisect its own payload
 * to find out which.
 *
 * A shape carrying `label` comes back as two elements, already wired to each
 * other in both directions.
 */
export function normalizeElements(input: unknown): NormalizedElement[] {
  if (!Array.isArray(input)) {
    throw new DrawingElementError("elements must be an array");
  }

  const out: NormalizedElement[] = [];
  input.forEach((raw, i) => {
    let el: NormalizedElement;
    try {
      el = normalizeElement(raw);
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      throw new DrawingElementError(`element ${i}: ${reason}`);
    }

    const label = (raw as Record<string, unknown>)?.label;
    if (typeof label !== "string" || label.trim() === "") {
      out.push(el);
      return;
    }
    if (el.type === "text") {
      throw new DrawingElementError(
        `element ${i}: a text element carries its words in \`text\`, not \`label\``,
      );
    }
    const refusal = labelRefusal(el.type);
    if (refusal) throw new DrawingElementError(`element ${i}: ${refusal}`);

    // An arrow's label sits on the line, and the arrow keeps the geometry it
    // was given: sizing it like a box grew a flat arrow 48px tall and wrapped
    // its words to a box that is not there.
    if (el.type === "arrow") {
      const text = labelElementFor(el, label);
      const bound = Array.isArray(el.boundElements) ? el.boundElements : [];
      out.push({ ...el, boundElements: [...bound, { id: text.id, type: "text" }] });
      out.push(text);
      return;
    }

    // Size the box to the label, unless the caller sized it themselves. A
    // shape left at the default width held a label wider than itself and the
    // words ran over the border, with nothing to say so.
    const source = raw as Record<string, unknown>;
    const sized = sizeToLabel(
      el,
      label,
      typeof source.width === "number",
      typeof source.height === "number",
    );

    const text = labelFor(sized, label);
    const bound = Array.isArray(sized.boundElements) ? sized.boundElements : [];
    out.push({ ...sized, boundElements: [...bound, { id: text.id, type: "text" }] });
    out.push(text);
  });

  return out;
}

/** A partial change to an element that already exists. Only `id` is required
 * and only supplied fields are carried. */
export interface ElementPatch {
  id: string;
  [key: string]: unknown;
}

/** Fields whose value must be a finite number when supplied. */
const NUMERIC_FIELDS = new Set([
  "x",
  "y",
  "width",
  "height",
  "angle",
  "strokeWidth",
  "roughness",
  "opacity",
  "fontSize",
  "fontFamily",
  "lineHeight",
  "z",
]);

/** Fields an update may not set: the server keeps them so that every canvas
 * takes the change. */
const SERVER_OWNED_FIELDS = new Set(["version", "versionNonce", "index", "updated"]);

/** A line's points: pairs of finite numbers, relative to the element's x/y. */
function points(value: unknown, i: number): number[][] {
  const ok =
    Array.isArray(value) &&
    value.every(
      (p) => Array.isArray(p) && p.length === 2 && p.every((n) => typeof n === "number" && Number.isFinite(n)),
    );
  if (!ok) {
    throw new DrawingElementError(`element ${i}: points must be a list of [x, y] number pairs`);
  }
  return value as number[][];
}

/** A shape's back-references: what is bound to it, as {id, type}. */
function boundElements(value: unknown, i: number): { id: string; type: "text" | "arrow" }[] | null {
  if (value === null) return null;
  const ok =
    Array.isArray(value) &&
    value.every(
      (b) =>
        typeof b === "object" &&
        b !== null &&
        typeof (b as { id?: unknown }).id === "string" &&
        ["text", "arrow"].includes((b as { type?: unknown }).type as string),
    );
  if (!ok) {
    throw new DrawingElementError(
      `element ${i}: boundElements must be null or a list of {id, type} with type "text" or "arrow"`,
    );
  }
  return value as { id: string; type: "text" | "arrow" }[];
}

/**
 * Normalize an update.
 *
 * Deliberately NOT normalizeElement: that fills every absent field with a
 * default, which is right for a new shape and destroys an existing one. An
 * update that set only `x` used to hand back a complete element carrying
 * y = 0, width = 100, height = 100 and no boundElements, and the merge
 * overwrote the shape with it — the caller moved a box and silently reset its
 * size and detached its label, while the result said applied: 1.
 *
 * So a patch carries exactly the keys it was given. `type` is not required —
 * the element is identified by id and already has one — and `text` may be
 * anything, since only a brand-new text element needs words to exist at all.
 */
export function normalizePatches(input: unknown): ElementPatch[] {
  if (!Array.isArray(input)) {
    throw new DrawingElementError("elements must be an array");
  }

  return input.map((raw, i) => {
    if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
      throw new DrawingElementError(`element ${i}: each element must be an object`);
    }
    const el = raw as Record<string, unknown>;
    const id = el.id;
    if (typeof id !== "string" || id === "") {
      throw new DrawingElementError(
        `element ${i}: an update names the shape to change by id — read the drawing to get them`,
      );
    }

    const patch: ElementPatch = { id };
    for (const [key, value] of Object.entries(el)) {
      if (key === "id" || value === undefined) continue;
      // The server's bookkeeping, not the caller's. An agent that read the
      // full view and sent it back carries the versions it read; taking them
      // would write an element below the room's copy, which no canvas takes.
      if (SERVER_OWNED_FIELDS.has(key)) continue;
      if (key === "isDeleted") {
        throw new DrawingElementError(
          `element ${i}: an update cannot erase or restore a shape — use mode:delete to erase it, ` +
            `or add it again to bring it back`,
        );
      }
      if (key === "type") {
        if (typeof value !== "string" || !SHAPE_TYPES.has(value)) {
          throw new DrawingElementError(
            `element ${i}: unknown element type ${JSON.stringify(value)}; supported: ${[...SHAPE_TYPES].join(", ")}`,
          );
        }
        patch.type = value;
        continue;
      }
      if (key === "points") {
        patch.points = points(value, i);
        continue;
      }
      if (key === "boundElements") {
        patch.boundElements = boundElements(value, i);
        continue;
      }
      if (NUMERIC_FIELDS.has(key)) {
        patch[key] = num(value, 0, `element ${i}: ${key}`);
        continue;
      }
      if (key === "startBinding" || key === "endBinding") {
        patch[key] = binding(value);
        continue;
      }
      patch[key] = value;
    }
    return patch;
  });
}
