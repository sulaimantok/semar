import { settingsUi } from "../../../../constants/ui-styles";

export const connectionCardStyles = {
  ...settingsUi,
  facts: "grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm",
  factLabel: "text-muted",
  factValue: "min-w-0 break-all",
  versions: "flex flex-wrap gap-1",
} as const;
