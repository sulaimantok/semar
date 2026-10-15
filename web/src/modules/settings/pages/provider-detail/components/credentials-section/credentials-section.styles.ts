import { settingsUi } from "../../../../constants/ui-styles";

export const credentialsSectionStyles = {
  ...settingsUi,
  srOnly: "sr-only",
  secretLegend: "text-sm font-medium sm:col-span-2",
  note: "text-xs text-muted sm:col-span-2",
  rowActions: "text-right",
} as const;
