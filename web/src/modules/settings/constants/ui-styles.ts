/**
 * Style tokens shared by every Settings page. Page style files spread these
 * and add their own, so the pages stay visually identical.
 */
export const settingsUi = {
  section: "flex flex-col gap-4",
  sectionHeader: "flex flex-wrap items-end justify-between gap-3",
  sectionTitle: "text-lg font-semibold tracking-tight",
  sectionHint: "text-sm text-muted",
  card: "flex flex-col gap-4 rounded-xl border border-border bg-panel p-5",
  form: "grid gap-4 sm:grid-cols-2",
  formWide: "sm:col-span-2",
  actions: "flex flex-wrap items-center gap-2 sm:col-span-2",
  input:
    "w-full rounded-md border border-border bg-surface px-3 py-2 text-sm outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/30",
  primary:
    "rounded-md bg-accent px-3 py-2 text-sm font-medium text-white hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60",
  secondary:
    "rounded-md border border-border bg-panel px-3 py-1.5 text-sm hover:bg-surface focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50",
  danger:
    "rounded-md border border-danger/40 px-3 py-1.5 text-sm text-danger hover:bg-danger/10 focus-visible:outline-2 focus-visible:outline-danger disabled:opacity-50",
  tableWrap: "overflow-x-auto rounded-xl border border-border bg-panel",
  table: "w-full min-w-[40rem] text-left text-sm",
  th: "border-b border-border px-4 py-2.5 text-xs font-medium uppercase tracking-wider text-muted",
  td: "border-b border-border px-4 py-2.5 align-middle",
  name: "font-medium",
  mono: "font-mono text-xs",
  muted: "text-muted",
  badge: "rounded px-1.5 py-0.5 text-xs bg-surface text-muted",
  link: "font-medium text-accent hover:underline focus-visible:outline-2 focus-visible:outline-accent",
  empty: "rounded-xl border border-dashed border-border p-8 text-center text-sm text-muted",
  success: "rounded-md border border-ok/40 bg-ok/10 px-3 py-2 text-sm",
} as const;
