export const globalFilterBarStyles = {
  root: "flex flex-wrap items-center gap-2 border-b border-border bg-panel px-4 py-2 md:px-8",
  label: "text-xs font-medium uppercase tracking-wider text-muted",
  select:
    "rounded-md border border-border bg-surface px-2.5 py-1 text-sm focus-visible:outline-2 focus-visible:outline-accent",
  selectActive: "border-accent text-accent",
  active: "inline-flex items-center gap-1.5 rounded-full bg-accent/10 px-2.5 py-0.5 text-xs font-medium text-accent",
  dot: "size-1.5 rounded-full bg-accent",
  reset:
    "rounded-md px-2 py-1 text-sm text-muted hover:bg-surface hover:text-text focus-visible:outline-2 focus-visible:outline-accent",
} as const;
