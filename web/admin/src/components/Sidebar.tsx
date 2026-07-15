import { useState } from "preact/hooks";
import type { Route } from "../router";
import { navigate } from "../router";

type NavItem = { route: string; label: string; icon: string; match: Route["name"][] };
type NavGroup = { label: string; items: NavItem[] };

const NAV_GROUPS: NavGroup[] = [
  {
    label: "Обзор",
    items: [{ route: "dashboard", label: "Дашборд", icon: "◧", match: ["dashboard"] }],
  },
  {
    label: "Пользователи",
    items: [{ route: "users", label: "Пользователи", icon: "◍", match: ["users", "user-detail"] }],
  },
  {
    label: "Коммерция",
    items: [
      { route: "orders", label: "Заказы", icon: "▤", match: ["orders"] },
      { route: "broadcast", label: "Рассылка", icon: "◎", match: ["broadcast"] },
      { route: "referrals", label: "Рефералы", icon: "◐", match: ["referrals"] },
    ],
  },
  {
    label: "Активность",
    items: [
      { route: "activity", label: "Активность", icon: "◔", match: ["activity"] },
      { route: "audit", label: "Аудит-лог", icon: "▥", match: ["audit"] },
    ],
  },
  {
    label: "Система",
    items: [
      { route: "webhooks", label: "Вебхуки", icon: "◫", match: ["webhooks"] },
      { route: "system", label: "Система", icon: "◈", match: ["system"] },
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
          <div class="sidebar-brand-mark" />
          {!collapsed && <span>Remnawave Admin</span>}
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
              >
                <span class="nav-item-icon">{item.icon}</span>
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
          {collapsed ? "»" : "« Свернуть"}
        </button>
      </nav>
    </>
  );
}
