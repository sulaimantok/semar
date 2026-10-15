export const settingsKeys = {
  all: ["settings"] as const,
  sites: () => [...settingsKeys.all, "sites"] as const,
  projects: () => [...settingsKeys.all, "projects"] as const,
  mappings: (projectId: string) => [...settingsKeys.projects(), projectId, "mappings"] as const,
  providers: () => [...settingsKeys.all, "providers"] as const,
  provider: (providerId: string) => [...settingsKeys.providers(), providerId] as const,
  syncStatus: (providerId: string) => [...settingsKeys.provider(providerId), "sync-status"] as const,
  credentials: (providerId: string) => [...settingsKeys.provider(providerId), "credentials"] as const,
} as const;
