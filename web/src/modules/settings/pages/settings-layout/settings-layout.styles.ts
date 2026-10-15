export const settingsLayoutStyles = {
  page: "flex max-w-6xl flex-col gap-6",
  header: "flex flex-col gap-1",
  title: "text-2xl font-semibold tracking-tight",
  subtitle: "text-sm text-muted",
  tabs: "flex gap-1 border-b border-border",
  tab: "-mb-px border-b-2 border-transparent px-3 py-2 text-sm text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-accent",
  tabActive: "border-accent font-medium text-text",
  readOnly: "rounded-md border border-border bg-surface px-3 py-2 text-sm text-muted",
} as const;
