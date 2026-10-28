import { useState } from "react"
import { useQueries, useQuery } from "@tanstack/react-query"
import { DicesIcon, RotateCcwIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { SearchInput } from "@/components/ui/search-input"
import { Slider } from "@/components/ui/slider"
import {
  candidates,
  drawRandomness,
  drawsFrom,
  formatCombinations,
  fromDrawn,
  log10Combinations,
  MAX_DIGITS,
  MAX_SEGMENTS,
  MAX_WORDS,
  pool,
  PRESETS,
  settingsOf,
  type Divider,
  type GeneratorSettings,
  type Joiner,
} from "@/lib/word-generator"
import { fetchWordlist, fetchWordlistCatalog, WORDLIST_CREDITS } from "@/lib/wordlists"
import { useWordGeneratorStore } from "@/stores/word-generator"
import { Chip, Choices } from "./generator-parts"

const JOINERS: { value: Joiner; label: string }[] = [
  { value: "-", label: "-" },
  { value: "_", label: "_" },
  { value: "", label: "none" },
  { value: "camel", label: "camelCase" },
]
const DIVIDERS: Divider[] = ["/", "-", ".", "_"]
const CANDIDATES = 5
// The lists stay as long as the page: they are static files.
const FOREVER = { staleTime: Infinity, gcTime: Infinity }

/**
 * "Generate" helper for string fields of a format the generator serves
 * (PRESETS, e.g. "path"): values made of words from the shipped word lists,
 * in a shape set with chips and sliders. Five candidates at a time, each one
 * that fits the field (fits: its pattern, its length); a click uses one.
 * The settings are kept in this browser by format, so the generator opens
 * as it was left.
 */
export function WordGenerator({
  format,
  fits,
  onUse,
  disabled,
}: {
  format: string
  fits: (value: string) => boolean
  onUse: (value: string) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  // Random numbers, drawn when the generator opens and on Shuffle; the
  // candidates are read from them, so rendering stays pure.
  const [drawn, setDrawn] = useState<Uint32Array | null>(null)
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) setDrawn(drawRandomness())
        setOpen(next)
      }}
    >
      <PopoverTrigger
        render={
          <Button type="button" variant="outline" disabled={disabled} title="Generate a value from words">
            <DicesIcon />
            Generate
          </Button>
        }
      />
      <PopoverContent align="end" className="w-[46rem] max-w-[calc(100vw-2rem)] gap-0 p-0">
        {open && drawn && (
          <Panel
            format={format}
            fits={fits}
            drawn={drawn}
            onShuffle={() => setDrawn(drawRandomness())}
            onUse={(value) => {
              onUse(value)
              setOpen(false)
            }}
          />
        )}
      </PopoverContent>
    </Popover>
  )
}

function Panel({
  format,
  fits,
  drawn,
  onShuffle,
  onUse,
}: {
  format: string
  fits: (value: string) => boolean
  drawn: Uint32Array
  onShuffle: () => void
  onUse: (value: string) => void
}) {
  const saved = useWordGeneratorStore((st) => st.saved[format])
  const save = useWordGeneratorStore((st) => st.save)
  const forget = useWordGeneratorStore((st) => st.forget)
  const s = settingsOf(saved, PRESETS[format] ?? PRESETS.path)
  const set = (patch: Partial<GeneratorSettings>) => save(format, { ...s, ...patch })
  const [query, setQuery] = useState("")

  const catalog = useQuery({ queryKey: ["wordlists"], queryFn: fetchWordlistCatalog, ...FOREVER })
  const lists = catalog.data?.lists ?? []
  const from = drawsFrom(s, lists)
  const ids = [...new Set([...from.first, ...from.last])]
  const loaded = useQueries({
    queries: ids.map((id) => ({ queryKey: ["wordlist", id], queryFn: () => fetchWordlist(id), ...FOREVER })),
  })
  const words = new Map(ids.map((id, i) => [id, loaded[i]?.data]))
  const ready = catalog.isSuccess && ids.every((id) => words.get(id))
  const failed = catalog.isError || loaded.some((q) => q.isError)
  const pools = {
    first: pool(from.first.map((id) => words.get(id) ?? [])),
    last: pool(from.last.map((id) => words.get(id) ?? [])),
  }
  const values = ready ? candidates(s, pools, CANDIDATES, fits, fromDrawn(drawn)) : []
  const log10 = log10Combinations(s, { first: pools.first.length, last: pools.last.length })

  const shown = lists.filter((l) => l.name.toLowerCase().includes(query.trim().toLowerCase()))
  const sourcesOf = (ids: readonly string[]) => ids.map((id) => catalog.data?.sources[id]?.name ?? id).join("; ")

  return (
    <div className="flex flex-col">
      <div className="grid gap-4 p-3 sm:grid-cols-2">
        <div className="flex flex-col gap-3">
          <Choices label="Words">
            <Chip active={s.pattern === "any"} onClick={() => set({ pattern: "any" })}>
              Any words
            </Chip>
            <Chip
              active={s.pattern === "adjective-noun"}
              onClick={() => set({ pattern: "adjective-noun" })}
              title="In each segment, adjectives and then a noun: quiet-river"
            >
              Adjective + noun
            </Chip>
          </Choices>
          <SliderRow label="Segments" value={s.segments} min={1} max={MAX_SEGMENTS} onChange={(segments) => set({ segments })} />
          <SliderRow
            label="Words in a segment"
            value={s.words}
            min={1}
            max={MAX_WORDS}
            onChange={(words) => set({ words })}
          />
          <SliderRow
            label="Number at the end"
            value={s.digits}
            min={0}
            max={MAX_DIGITS}
            onChange={(digits) => set({ digits })}
            text={(d) => (d === 0 ? "none" : `${d} digit${d === 1 ? "" : "s"}`)}
          />
          <Choices label="Join words with">
            {JOINERS.map((j) => (
              <Chip key={j.label} active={s.joiner === j.value} onClick={() => set({ joiner: j.value })}>
                <span className={j.value === "" || j.value === "camel" ? undefined : "font-mono"}>{j.label}</span>
              </Chip>
            ))}
          </Choices>
          <Choices label="Divider">
            {DIVIDERS.map((d) => (
              <Chip key={d} active={s.divider === d} onClick={() => set({ divider: d })}>
                <span className="font-mono">{d}</span>
              </Chip>
            ))}
            <Chip active={s.leading} onClick={() => set({ leading: !s.leading })} title="A divider before the first segment">
              Before
            </Chip>
            <Chip active={s.trailing} onClick={() => set({ trailing: !s.trailing })} title="A divider after the last segment">
              After
            </Chip>
          </Choices>
          <Choices label="Case">
            <Chip active={s.case === "lower"} onClick={() => set({ case: "lower" })}>
              lower
            </Chip>
            <Chip active={s.case === "title"} onClick={() => set({ case: "title" })}>
              Title
            </Chip>
          </Choices>
        </div>

        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">Word lists</span>
            <SearchInput
              value={query}
              onValueChange={setQuery}
              placeholder="Search lists…"
              debounceMs={0}
              title=""
              className="relative w-full"
            />
            <div className="h-44 overflow-y-auto rounded-md border p-1">
              {catalog.isPending && <p className="px-2 py-1 text-sm text-muted-foreground">Loading the word lists…</p>}
              {shown.map((l) => {
                const on = s.lists.includes(l.id)
                return (
                  <label
                    key={l.id}
                    title={`From ${sourcesOf(l.sources)}`}
                    className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted/60"
                  >
                    {/* The last one picked stays: a value needs words. */}
                    <Checkbox
                      checked={on}
                      disabled={on && s.lists.length === 1}
                      onCheckedChange={(checked) =>
                        set({ lists: checked ? [...s.lists, l.id] : s.lists.filter((id) => id !== l.id) })
                      }
                    />
                    <span className="min-w-0 flex-1 truncate">{l.name}</span>
                    {l.theme && <span className="text-xs text-muted-foreground">theme</span>}
                    <span className="text-xs text-muted-foreground tabular-nums">{l.count.toLocaleString("en-US")}</span>
                  </label>
                )
              })}
            </div>
            {s.pattern === "adjective-noun" && (
              <p className="text-xs text-muted-foreground">
                Adjectives come from the adjective lists picked (Adjectives, Colors), nouns from the others; with none of
                a kind picked, from Adjectives or Nouns.
              </p>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">Pick one</span>
            {failed ? (
              <p className="text-sm text-destructive">The word lists did not load.</p>
            ) : !ready ? (
              <p className="text-sm text-muted-foreground">Loading the words…</p>
            ) : values.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No value made this way fits the field: its pattern or its length. Change the shape.
              </p>
            ) : (
              values.map((v) => (
                <button
                  key={v}
                  type="button"
                  onClick={() => onUse(v)}
                  className="truncate rounded-md border px-2.5 py-1.5 text-left font-mono text-sm transition-colors hover:bg-muted"
                >
                  {v}
                </button>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-3 py-2 text-xs text-muted-foreground">
        {ready && (
          <span className="tabular-nums">
            {log10 >= 6 && "≈ "}
            {formatCombinations(log10)} possible values
          </span>
        )}
        <a href={WORDLIST_CREDITS} target="_blank" rel="noreferrer" className="underline-offset-2 hover:underline">
          Word list credits
        </a>
        <span className="ml-auto flex items-center gap-1">
          <Button type="button" variant="ghost" size="sm" onClick={() => forget(format)} title="Back to this field's defaults">
            <RotateCcwIcon />
            Reset
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={onShuffle}>
            <DicesIcon />
            Shuffle
          </Button>
        </span>
      </div>
    </div>
  )
}

function SliderRow({
  label,
  value,
  min,
  max,
  onChange,
  text = String,
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
  text?: (value: number) => string
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="font-medium text-muted-foreground">{label}</span>
        <span className="tabular-nums">{text(value)}</span>
      </div>
      <Slider value={value} min={min} max={max} step={1} onValueChange={onChange} aria-label={label} />
    </div>
  )
}
