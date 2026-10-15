export const capacityOverviewStyles = {
  section: "flex flex-col gap-4",
  heading: "flex flex-col gap-1",
  title: "text-lg font-semibold tracking-tight",
  subtitle: "text-sm text-muted",
  grid: "grid gap-4 sm:grid-cols-2 lg:grid-cols-3",
  card: "flex flex-col gap-3 rounded-xl border border-border bg-panel p-5",
  cardTitle: "text-xs font-medium uppercase tracking-wider text-muted",
  stat: "text-3xl font-semibold tabular-nums",
  statUnit: "ml-1 text-sm font-normal text-muted",
  hint: "text-sm text-muted",
  // Native <progress>, styled through its vendor pseudo-elements.
  meter:
    "h-2 w-full appearance-none overflow-hidden rounded-full bg-border [&::-moz-progress-bar]:bg-accent [&::-webkit-progress-bar]:bg-border [&::-webkit-progress-value]:rounded-full [&::-webkit-progress-value]:bg-accent",
  meterRow: "flex flex-col gap-1.5",
  meterLabel: "flex justify-between text-sm",
  breakdown: "flex flex-wrap gap-2",
  chip: "rounded-md border border-border px-2 py-0.5 text-xs tabular-nums",
  error: "text-sm text-danger",
  empty:
    "rounded-xl border border-dashed border-border p-8 text-center text-sm text-muted",
} as const;
