// Shared tooltip body for the operation switcher's trigger and its list rows,
// so both read the same way. Stacked rather than inline because a description
// is a sentence, not a label — the tooltip wraps at its own max-width and the
// name stays scannable as the first line.
export function OperationTooltipBody({
  name,
  description,
}: {
  name: string;
  description?: string | null;
}) {
  return (
    <span className="flex flex-col gap-0.5 text-left">
      <span className="font-medium">{name}</span>
      {description ? (
        <span className="text-background/70">{description}</span>
      ) : null}
    </span>
  );
}
