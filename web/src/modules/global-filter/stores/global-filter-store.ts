/**
 * The global Site / Project filter (roadmap §3.5). It is UI state, not
 * server data: a per-viewer choice kept in localStorage so it survives
 * reloads. Storage may be unavailable (private mode), so every access is
 * guarded and the filter falls back to "All".
 */
export interface IGlobalFilter {
  siteId?: string;
  projectId?: string;
}

const STORAGE_KEY = "semar.global-filter.v1";

function load(): IGlobalFilter {
  try {
    const raw = globalThis.localStorage?.getItem(STORAGE_KEY);
    if (!raw) return {};
    const v = JSON.parse(raw) as unknown;
    if (typeof v !== "object" || v === null) return {};
    const { siteId, projectId } = v as Record<string, unknown>;
    return {
      siteId: typeof siteId === "string" && siteId ? siteId : undefined,
      projectId: typeof projectId === "string" && projectId ? projectId : undefined,
    };
  } catch {
    return {};
  }
}

function save(f: IGlobalFilter) {
  try {
    globalThis.localStorage?.setItem(STORAGE_KEY, JSON.stringify(f));
  } catch {
    // Not persisted; the filter still applies for this session.
  }
}

let state: IGlobalFilter = load();
const listeners = new Set<() => void>();

export const globalFilterStore = {
  get: (): IGlobalFilter => state,
  subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
  set(patch: IGlobalFilter) {
    state = { ...state, ...patch };
    if (!state.siteId) delete state.siteId;
    if (!state.projectId) delete state.projectId;
    save(state);
    listeners.forEach((l) => l());
  },
  reset() {
    globalFilterStore.set({ siteId: undefined, projectId: undefined });
  },
  /** Re-reads storage; for tests. */
  reload() {
    state = load();
    listeners.forEach((l) => l());
  },
};
