import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";

import { useMe } from "../../hooks/use-me";

import { requireAuthStyles as styles } from "./require-auth.styles";

/** Renders children for signed-in users and sends others to /login. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { data: me, isPending, isError } = useMe();
  const location = useLocation();

  if (isPending) return <div className={styles.pending}>Loading…</div>;
  if (isError || !me) {
    const next = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?next=${next}`} replace />;
  }
  return children;
}
