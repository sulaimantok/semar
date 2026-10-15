export const globalFilterKeys = {
  all: ["global-filter"] as const,
  sites: () => [...globalFilterKeys.all, "sites"] as const,
  projects: () => [...globalFilterKeys.all, "projects"] as const,
} as const;
