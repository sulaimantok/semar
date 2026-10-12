export const systemKeys = {
  all: ["system"] as const,
  version: () => [...systemKeys.all, "version"] as const,
  readiness: () => [...systemKeys.all, "readiness"] as const,
} as const;
