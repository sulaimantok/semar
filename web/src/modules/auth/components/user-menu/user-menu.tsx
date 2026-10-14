import { LogOut } from "lucide-react";
import { useNavigate } from "react-router";

import { useLogout } from "../../hooks/use-logout";
import { useMe } from "../../hooks/use-me";

import { userMenuStyles as styles } from "./user-menu.styles";

export function UserMenu() {
  const { data: me } = useMe();
  const logout = useLogout();
  const navigate = useNavigate();
  if (!me) return null;

  const roles = me.roles.map((r) => (r.projectId ? `${r.role} · ${r.projectId}` : r.role)).join(", ");
  return (
    <div className={styles.root}>
      <div className={styles.who}>
        <div className={styles.name}>{me.username}</div>
        <div className={styles.role}>{roles || "no roles"}</div>
      </div>
      <button
        type="button"
        className={styles.logout}
        disabled={logout.isPending}
        onClick={() => logout.mutate(undefined, { onSettled: () => navigate("/login", { replace: true }) })}
        aria-label="Sign out"
      >
        <LogOut className={styles.logoutIcon} aria-hidden="true" />
      </button>
    </div>
  );
}
