import { settingsUi } from "../../../../constants/ui-styles";

export const editProviderFormStyles = {
  ...settingsUi,
  textarea:
    "min-h-20 w-full rounded-md border border-border bg-surface px-3 py-2 font-mono text-xs outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/30",
  note: "text-xs text-muted sm:col-span-2",
} as const;
