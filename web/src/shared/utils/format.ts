/** Human-readable age of a timestamp, e.g. "40 s ago", "3 min ago". */
export function formatAge(at: Date | string, now: Date = new Date()): string {
  const then = typeof at === "string" ? new Date(at) : at;
  const seconds = Math.max(0, Math.round((now.getTime() - then.getTime()) / 1000));
  if (seconds < 60) return `${seconds} s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours} h ago`;
  return `${Math.round(hours / 24)} d ago`;
}

/** Memory in MiB as MiB or GiB, e.g. 512 → "512 MiB", 4096 → "4 GiB". */
export function formatMemoryMiB(mib: number): string {
  if (mib < 1024) return `${mib} MiB`;
  const gib = mib / 1024;
  return `${Number.isInteger(gib) ? gib : gib.toFixed(1)} GiB`;
}
