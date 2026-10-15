import type { IResource } from "../services/compute-service";

/** Detail route of a VM. External IDs are provider-defined, so both parts are encoded. */
export function vmPath(vm: Pick<IResource, "providerId" | "externalId">): string {
  return `/compute/vms/${encodeURIComponent(vm.providerId)}/${encodeURIComponent(vm.externalId)}`;
}
