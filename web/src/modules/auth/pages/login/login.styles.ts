export const loginStyles = {
  page: "grid min-h-dvh place-items-center bg-surface px-4",
  card: "flex w-full max-w-sm flex-col gap-5 rounded-xl border border-border bg-panel p-6",
  header: "flex flex-col gap-1",
  title: "text-xl font-semibold tracking-tight",
  subtitle: "text-sm text-muted",
  form: "flex flex-col gap-4",
  field: "flex flex-col gap-1.5",
  label: "text-sm font-medium",
  input:
    "rounded-md border border-border bg-surface px-3 py-2 text-sm outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/30",
  error: "rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger",
  submit:
    "rounded-md bg-accent px-3 py-2 text-sm font-medium text-white hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-60",
} as const;
