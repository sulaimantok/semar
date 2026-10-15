export const computeKeys = {
  all: ["compute"] as const,
  vms: () => [...computeKeys.all, "vms"] as const,
  vmList: (providerId: string | undefined) => [...computeKeys.vms(), { providerId: providerId ?? null }] as const,
  providers: () => [...computeKeys.all, "providers"] as const,
  capacity: (q: { providerId?: string; projectId?: string }) =>
    [...computeKeys.all, "capacity", { providerId: q.providerId ?? null, projectId: q.projectId ?? null }] as const,
} as const;
