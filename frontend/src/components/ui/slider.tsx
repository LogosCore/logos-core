import { Slider as SliderPrimitive } from "@base-ui/react/slider"

import { cn } from "@/lib/utils"

/** A slider of one value; valueText, when given, is what a screen reader says for a value. */
function Slider({
  className,
  valueText,
  ...props
}: SliderPrimitive.Root.Props<number> & { valueText?: (value: number) => string }) {
  return (
    <SliderPrimitive.Root data-slot="slider" thumbAlignment="edge" className={cn("w-full", className)} {...props}>
      <SliderPrimitive.Control className="relative flex h-5 w-full touch-none items-center select-none data-disabled:opacity-50">
        <SliderPrimitive.Track className="relative h-1 w-full grow overflow-hidden rounded-full bg-muted select-none">
          <SliderPrimitive.Indicator className="h-full bg-primary select-none" />
        </SliderPrimitive.Track>
        <SliderPrimitive.Thumb
          getAriaValueText={valueText ? (_, value) => valueText(value) : undefined}
          className="block size-4 shrink-0 rounded-full border border-primary bg-background shadow-sm ring-ring/50 transition-[box-shadow] select-none hover:ring-3 focus-visible:ring-3 focus-visible:outline-hidden" />
      </SliderPrimitive.Control>
    </SliderPrimitive.Root>
  )
}

export { Slider }
