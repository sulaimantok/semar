import { settingsUi } from "../../constants/ui-styles";

export const providerDetailStyles = {
  ...settingsUi,
  back: "text-sm text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-accent",
  titleRow: "flex flex-wrap items-center justify-between gap-3",
  titleActions: "flex flex-wrap items-center gap-2",
  title: "text-xl font-semibold tracking-tight",
  grid: "grid gap-4 lg:grid-cols-2",
  facts: "grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm",
  factLabel: "text-muted",
  factValue: "break-all",
  status: "inline-flex items-center gap-1.5 font-medium",
  dot: "size-2 rounded-full",
  dotHealthy: "bg-ok",
  dotDegraded: "bg-warn",
  dotUnreachable: "bg-danger",
  errorText: "font-mono text-xs text-danger",
  confirm: "flex flex-wrap items-center gap-2 text-sm",
} as const;
