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
  mappingEdit: "rounded-md border border-border p-3",
  rowActions: "ml-auto flex flex-wrap items-center gap-2",
  linkButton: "text-sm text-accent hover:underline focus-visible:outline-2 focus-visible:outline-accent",
  linkDanger: "text-sm text-danger hover:underline focus-visible:outline-2 focus-visible:outline-danger",
} as const;
