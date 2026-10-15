import { settingsUi } from "../../constants/ui-styles";

export const providersStyles = {
  ...settingsUi,
  endpoints: "flex flex-col gap-0.5 font-mono text-xs",
  textarea:
    "min-h-20 w-full rounded-md border border-border bg-surface px-3 py-2 font-mono text-xs outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/30",
} as const;
