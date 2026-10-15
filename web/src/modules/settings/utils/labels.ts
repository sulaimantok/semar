/** Parses "env=prod, tier=1" into labels; returns null on a malformed pair. */
export function parseLabels(input: string): Record<string, string> | null {
  const labels: Record<string, string> = {};
  for (const part of input.split(",")) {
    const pair = part.trim();
    if (!pair) continue;
    const eq = pair.indexOf("=");
    if (eq <= 0) return null;
    labels[pair.slice(0, eq).trim()] = pair.slice(eq + 1).trim();
  }
  return labels;
}
