export const appShellStyles = {
  root: "min-h-dvh md:grid md:grid-cols-[15rem_1fr]",
  sidebar:
    "flex flex-col border-b border-border bg-panel md:sticky md:top-0 md:h-dvh md:border-b-0 md:border-r",
  footer: "mt-auto border-t border-border px-3 py-3",
  brand: "flex items-center gap-2 px-5 py-4",
  brandMark: "grid size-8 place-items-center rounded-lg bg-accent text-sm font-bold text-white",
  brandName: "text-base font-semibold tracking-wide",
  brandTag: "text-xs text-muted",
  nav: "flex gap-1 overflow-x-auto px-3 pb-3 md:flex-col md:overflow-visible",
  navLink:
    "flex items-center gap-3 rounded-md px-3 py-2 text-sm text-muted transition-colors hover:bg-surface hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
  navLinkActive: "bg-surface font-medium text-text",
  navIcon: "size-4 shrink-0",
  main: "min-w-0 px-4 py-6 md:px-8 md:py-8",
} as const;
