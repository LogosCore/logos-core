import data, { type EmojiMartData } from "@emoji-mart/data";
import { Picker } from "emoji-mart";
import { useEffect, useRef } from "react";
import { emojiNames, retitleEmojis } from "@/components/wiki/icon-code-names";

// Thin React wrapper around emoji-mart's vanilla Picker.
//
// Replaces the @emoji-mart/react package, which was a ~20-line wrapper doing
// exactly this. Dropping it removes the only React-19 peer-dependency conflict
// in the tree — its last release (1.1.1, January 2023) still declares
// `react@"^16.8 || ^17 || ^18"`, and upstream has published nothing since, so
// there was no version to upgrade to. Owning these twenty lines is cheaper than
// carrying `npm ci --legacy-peer-deps` everywhere forever.
//
// Two things this does that the upstream wrapper did not:
//   - Prop updates happen in an effect. Upstream called picker.update(props)
//     during render, mutating an external object mid-render, which is not safe
//     under concurrent rendering.
//   - onEmojiSelect is read through a ref, so a caller passing an inline arrow
//     (as the icon picker does) does not tear down and rebuild the picker on
//     every render.
//
// The vanilla Picker is a custom element: its constructor empties the host node
// and appends itself, and its disconnectedCallback unregisters the component
// when React removes the host. We clear the host on cleanup anyway so a
// remount never renders two pickers into the same node.
//
// The dataset is imported here rather than passed in so that the one lazy
// import of this module (see document-icon-picker.tsx) fetches both.

/** The subset of emoji-mart's Picker options this app uses. The library's own
 *  typings declare `constructor(props: any)`, so this interface — not the
 *  library — is what gives call sites type safety. Widen it deliberately if a
 *  new option is needed. */
export interface EmojiPickerProps {
  /** Fires with the chosen emoji; `native` is the character itself. */
  onEmojiSelect: (emoji: EmojiSelection) => void;
  theme: "light" | "dark";
  previewPosition?: "none" | "top" | "bottom";
  skinTonePosition?: "none" | "preview" | "search";
  /** An emoji's tooltip, from its name and the emoji itself: in place of
   *  emoji-mart's (its name alone, while the preview is off). Read once, when
   *  the picker is built. */
  titleOf?: (name: string, native: string) => string;
}

/** Emoji-mart emits a much larger object; these are the fields we rely on. */
export interface EmojiSelection {
  native: string;
  id: string;
}

/** Structural type for the constructed picker — the library exports the class
 *  but types its instances loosely. It is a custom element with an open shadow
 *  root, attached by its constructor, which it draws in. */
interface PickerInstance {
  update: (props: Record<string, unknown>) => void;
  shadowRoot: ShadowRoot | null;
}

// emoji-mart's names by emoji, built the first time a picker titles its emoji.
// The dataset's typings declare its interfaces but not the default export,
// which at run time is the JSON itself.
let names: Map<string, string> | undefined;
const dataset = data as unknown as EmojiMartData;

export function EmojiPicker({
  onEmojiSelect,
  theme,
  previewPosition = "none",
  skinTonePosition = "none",
  titleOf,
}: EmojiPickerProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const pickerRef = useRef<PickerInstance | null>(null);

  // Latest-callback ref: lets the picker be built once while still calling the
  // caller's current handler, even though that handler is a fresh closure on
  // every render.
  const onSelectRef = useRef(onEmojiSelect);
  useEffect(() => {
    onSelectRef.current = onEmojiSelect;
  }, [onEmojiSelect]);

  // Construction options are captured once. Everything that can change at
  // runtime is applied through update() in the effect below instead, so the
  // picker is never rebuilt (which would lose scroll position and focus).
  const initialRef = useRef({ theme, previewPosition, skinTonePosition, titleOf });

  useEffect(() => {
    const host = hostRef.current;
    if (host === null) return;

    const initial = initialRef.current;
    const picker = new Picker({
      data,
      theme: initial.theme,
      previewPosition: initial.previewPosition,
      skinTonePosition: initial.skinTonePosition,
      onEmojiSelect: (emoji: EmojiSelection) => onSelectRef.current(emoji),
      ref: hostRef,
    }) as unknown as PickerInstance;
    pickerRef.current = picker;

    // The caller's tooltips, on each emoji as emoji-mart draws it (the first
    // rows, rows scrolled to, search results), before the pointer arrives.
    // Not on a hover: Chromium fires no mouseover from one element to another
    // inside a shadow root, so a hover listener saw only the first emoji the
    // pointer reached. The records of the observer's own title writes are
    // dropped, so they start no pass of their own.
    let titles: MutationObserver | undefined;
    const root = picker.shadowRoot;
    const titleOf = initial.titleOf;
    if (titleOf && root !== null) {
      const named = (names ??= emojiNames(dataset));
      const retitle = () => {
        retitleEmojis(root, named, titleOf);
        titles?.takeRecords();
      };
      titles = new MutationObserver(retitle);
      titles.observe(root, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ["aria-label", "title"],
      });
      retitle();
    }

    return () => {
      titles?.disconnect();
      pickerRef.current = null;
      host.replaceChildren();
    };
  }, []);

  // Theme follows the app. update() takes a partial prop bag, so this repaints
  // the existing picker rather than constructing a new one.
  useEffect(() => {
    pickerRef.current?.update({ theme });
  }, [theme]);

  return <div ref={hostRef} />;
}
