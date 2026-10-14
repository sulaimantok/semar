export const userMenuStyles = {
  root: "flex items-center justify-between gap-2 px-2",
  who: "min-w-0",
  name: "truncate text-sm font-medium",
  role: "truncate text-xs text-muted",
  logout:
    "rounded-md px-2 py-1 text-xs text-muted hover:bg-surface hover:text-text focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50",
  logoutIcon: "size-4",
} as const;
