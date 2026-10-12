import {
  Activity,
  Boxes,
  HardDrive,
  LayoutDashboard,
  Network,
  Server,
  Settings,
  type LucideIcon,
} from "lucide-react";

export interface INavItem {
  label: string;
  to: string;
  icon: LucideIcon;
  /** Unreleased features are hidden, not shown empty (roadmap §3.5). */
  released: boolean;
}

/** Navigation by resource type (roadmap §3.5). */
export const NAV_ITEMS: readonly INavItem[] = [
  { label: "Overview", to: "/", icon: LayoutDashboard, released: true },
  { label: "Compute", to: "/compute", icon: Server, released: false },
  { label: "Kubernetes", to: "/kubernetes", icon: Boxes, released: false },
  { label: "Network", to: "/network", icon: Network, released: false },
  { label: "Storage", to: "/storage", icon: HardDrive, released: false },
  { label: "Observability", to: "/observability", icon: Activity, released: false },
  { label: "Settings", to: "/settings", icon: Settings, released: false },
];

export function visibleNavItems(items: readonly INavItem[] = NAV_ITEMS): INavItem[] {
  return items.filter((item) => item.released);
}
