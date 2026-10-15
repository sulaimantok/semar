import { settingsUi } from "../../constants/ui-styles";

export const projectsStyles = {
  ...settingsUi,
  list: "flex flex-col gap-3",
  projectHead: "flex flex-wrap items-center justify-between gap-2",
  projectName: "font-medium",
  mappingList: "flex flex-col gap-1 text-sm",
  mapping: "flex flex-wrap items-center gap-2",
  arrow: "text-muted",
  inlineForm: "grid gap-2 sm:grid-cols-[1fr_1fr_auto] sm:items-end",
} as const;
