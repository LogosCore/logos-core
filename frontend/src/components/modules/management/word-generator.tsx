import { useState } from "react"
import { useQueries, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { DicesIcon, RotateCcwIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Slider } from "@/components/ui/slider"
import {
  candidates,
  drawRandomness,
  drawsFrom,
  formatCombinations,
  fromDrawn,
  log10Combinations,
  MAX_SEGMENTS,
  pool,
  PRESETS,
  settingsOf,
  type Divider,
  type GeneratorSettings,
} from "@/lib/word-generator"
import { fetchWordlist, fetchWordlistCatalog, WORDLIST_CREDITS } from "@/lib/wordlists"
import { useWordGeneratorStore } from "@/stores/word-generator"
import { Chip, Choices } from "./generator-parts"

const DIVIDERS: Divider[] = ["/", "-", ".", "_"]
const CANDIDATES = 5
// The lists stay as long as the page: they are static files.
const FOREVER = { staleTime: Infinity, gcTime: Infinity }

/**
 * The picked lists' words as one pool, and how their loading goes. Out of
 * the component, so useQueries runs it again only when a list's result
 * changes: Directory names alone is 180,000 names, not to be merged again
 * on every slider move.
 */
function combineLists(results: UseQueryResult<string[]>[]) {
  return {
    words: pool(results.map((r) => r.data ?? [])),
    loading: results.some((r) => r.isPending),
    failed: results.some((r) => r.isError),
  }
}

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

  const queryClient = useQueryClient()
  const catalog = useQuery({ queryKey: ["wordlists"], queryFn: fetchWordlistCatalog, ...FOREVER })
  const lists = catalog.data?.lists ?? []
  const picked = drawsFrom(s, lists)
  const wordlist = (id: string) => ({ queryKey: ["wordlist", id], queryFn: () => fetchWordlist(id), ...FOREVER })
  // Made of the lists that are here: while a list just picked loads, the
  // candidates stay those of the others, and change once when it comes.
  const loaded = useQueries({ queries: picked.map(wordlist), combine: combineLists })
  const words = loaded.words
  const loading = catalog.isPending || loaded.loading
  const failed = catalog.isError || loaded.failed
  const values = candidates(s, words, CANDIDATES, fits, fromDrawn(drawn))
  const log10 = log10Combinations(s, words.length)
  // Fetched while the pointer is on a list, so it is mostly here by the click.
  const prefetch = (id: string) => void queryClient.prefetchQuery(wordlist(id))
  const sourcesOf = (ids: readonly string[]) => ids.map((id) => catalog.data?.sources[id]?.name ?? id).join("; ")

  return (
    <div className="flex flex-col">
      <div className="grid gap-4 p-3 sm:grid-cols-2">
        <div className="flex flex-col gap-3">
          <SliderRow label="Segments" value={s.segments} min={1} max={MAX_SEGMENTS} onChange={(segments) => set({ segments })} />
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

        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-xs font-medium text-muted-foreground">Word lists</span>
          <div className="rounded-md border p-1">
            {catalog.isPending && <p className="px-2 py-1 text-sm text-muted-foreground">Loading the word lists…</p>}
            {lists.map((l) => {
              const on = picked.includes(l.id)
              return (
                <label
                  key={l.id}
                  title={`From ${sourcesOf(l.sources)}`}
                  onPointerEnter={() => prefetch(l.id)}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted/60"
                >
                  {/* The last one picked stays: a value needs words. */}
                  <Checkbox
                    checked={on}
                    disabled={on && picked.length === 1}
                    onCheckedChange={(checked) =>
                      set({ lists: checked ? [...picked, l.id] : picked.filter((id) => id !== l.id) })
                    }
                  />
                  <span className="min-w-0 flex-1 truncate">{l.name}</span>
                  <span className="text-xs text-muted-foreground tabular-nums">{l.count.toLocaleString("en-US")}</span>
                </label>
              )
            })}
          </div>
        </div>
      </div>

      <div className="border-t p-3">
        {/* Across the whole generator, as tall as five candidates whatever
            it shows, so the popover keeps its size. */}
        <div className="flex min-h-[13.5rem] flex-col gap-1.5">
          <span className="text-xs font-medium text-muted-foreground">
            Pick one
            {loading && values.length > 0 && <span className="font-normal"> · loading words…</span>}
          </span>
          {values.length === 0 && failed ? (
            <p className="text-sm text-destructive">The word lists did not load.</p>
          ) : values.length === 0 && loading ? (
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

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-3 py-2 text-xs text-muted-foreground">
        {values.length > 0 && (
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
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2 text-xs">
        <span className="font-medium text-muted-foreground">{label}</span>
        <span className="tabular-nums">{value}</span>
      </div>
      <Slider value={value} min={min} max={max} step={1} onValueChange={onChange} aria-label={label} />
    </div>
  )
}
