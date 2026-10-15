import type { ICapacityQuery, IVmFilter } from "../services/compute-service";

export const computeKeys = {
  all: ["compute"] as const,
  vms: () => [...computeKeys.all, "vms"] as const,
  vmList: (f: IVmFilter) =>
    [
      ...computeKeys.vms(),
      { providerId: f.providerId ?? null, siteId: f.siteId ?? null, projectId: f.projectId ?? null, arch: f.arch ?? null },
    ] as const,
  vm: (providerId: string, externalId: string) => [...computeKeys.vms(), "detail", providerId, externalId] as const,
  providers: () => [...computeKeys.all, "providers"] as const,
  capacity: (q: ICapacityQuery) =>
    [
      ...computeKeys.all,
      "capacity",
      { providerId: q.providerId ?? null, siteId: q.siteId ?? null, projectId: q.projectId ?? null },
    ] as const,
} as const;
