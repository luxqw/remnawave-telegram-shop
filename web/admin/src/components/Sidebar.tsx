import { useState } from "preact/hooks";
import {
  LayoutDashboard,
  Users,
  Receipt,
  Megaphone,
  Share2,
  Activity,
  Bell,
  ScrollText,
  Webhook,
  Settings,
  ChevronsLeft,
  ChevronsRight,
  type LucideIcon,
} from "lucide-preact";
import type { Route } from "../router";
import { navigate } from "../router";
import logoUrl from "../assets/logo.png";

type NavItem = { route: string; label: string; icon: LucideIcon; match: Route["name"][] };
type NavGroup = { label: string; items: NavItem[] };

const NAV_GROUPS: NavGroup[] = [
  {
    label: "Обзор",
    items: [{ route: "dashboard", label: "Дашборд", icon: LayoutDashboard, match: ["dashboard"] }],
  },
  {
    label: "Пользователи",
    items: [{ route: "users", label: "Пользователи", icon: Users, match: ["users", "user-detail"] }],
  },
  {
    label: "Коммерция",
    items: [
      { route: "orders", label: "Заказы", icon: Receipt, match: ["orders"] },
      { route: "broadcast", label: "Рассылка", icon: Megaphone, match: ["broadcast"] },
      { route: "referrals", label: "Рефералы", icon: Share2, match: ["referrals"] },
    ],
  },
  {
    label: "Активность",
    items: [
      { route: "activity", label: "Активность", icon: Activity, match: ["activity"] },
      { route: "notifications", label: "Уведомления", icon: Bell, match: ["notifications"] },
      { route: "audit", label: "Аудит-лог", icon: ScrollText, match: ["audit"] },
    ],
  },
  {
    label: "Система",
    items: [
      { route: "webhooks", label: "Вебхуки", icon: Webhook, match: ["webhooks"] },
      { route: "system", label: "Система", icon: Settings, match: ["system"] },
    ],
  },
];

export function Sidebar(props: { current: Route; open: boolean; onNavigate: () => void }) {
  const [collapsed, setCollapsed] = useState(false);

  return (
    <>
      {props.open && <div class="sidebar-backdrop" onClick={props.onNavigate} />}
      <nav class={`sidebar ${collapsed ? "collapsed" : ""} ${props.open ? "open" : ""}`}>
        <div class="sidebar-brand">
          <img src={logoUrl} alt="Vexel VPN" class="sidebar-brand-mark" />
          {!collapsed && <span>Vexel VPN</span>}
        </div>
        {NAV_GROUPS.map((group) => (
          <div class="sidebar-group" key={group.label}>
            {!collapsed && <div class="sidebar-group-label">{group.label}</div>}
            {group.items.map((item) => (
              <a
                key={item.route}
                class={`nav-item ${item.match.includes(props.current.name) ? "active" : ""}`}
                onClick={(e) => {
                  e.preventDefault();
                  navigate(item.route);
                  props.onNavigate();
                }}
                href={`#/${item.route}`}
                aria-label={collapsed ? item.label : undefined}
              >
                <item.icon size={18} class="nav-item-icon" />
                {!collapsed && <span>{item.label}</span>}
              </a>
            ))}
          </div>
        ))}
        <button
          class="sidebar-toggle"
          onClick={() => setCollapsed((c) => !c)}
          aria-label={collapsed ? "Развернуть меню" : "Свернуть меню"}
        >
          {collapsed ? <ChevronsRight size={16} /> : <><ChevronsLeft size={16} /> Свернуть</>}
        </button>
      </nav>
    </>
  );
}
