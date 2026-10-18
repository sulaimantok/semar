export const vmConsoleStyles = {
  page: "flex flex-col gap-4",
  toolbar: "flex flex-wrap items-center justify-between gap-3",
  titleGroup: "flex flex-col gap-0.5",
  back: "text-sm text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-accent",
  title: "text-xl font-semibold tracking-tight",
  actions: "flex flex-wrap items-center gap-2",
  tab: "rounded-md border border-border px-3 py-1.5 text-sm hover:bg-surface focus-visible:outline-2 focus-visible:outline-accent",
  tabActive: "border-accent bg-accent/10 font-medium text-accent",
  button:
    "rounded-md border border-border bg-panel px-3 py-1.5 text-sm hover:bg-surface focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50",
  status: "inline-flex items-center gap-2 text-sm",
  dot: "size-2 rounded-full",
  dotOk: "bg-ok",
  dotWait: "bg-warn",
  dotClosed: "bg-border",
  error: "rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger",
  note: "text-xs text-muted",
} as const;
