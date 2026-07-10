import { useState } from "preact/hooks";
import type { Route } from "../router";
import { navigate } from "../router";

const NAV_ITEMS: { route: string; label: string; icon: string; match: Route["name"][] }[] = [
  { route: "dashboard", label: "Дашборд", icon: "◧", match: ["dashboard"] },
  { route: "users", label: "Пользователи", icon: "◍", match: ["users", "user-detail"] },
  { route: "orders", label: "Заказы", icon: "▤", match: ["orders"] },
  { route: "broadcast", label: "Рассылка", icon: "◎", match: ["broadcast"] },
  { route: "referrals", label: "Рефералы", icon: "◐", match: ["referrals"] },
  { route: "webhooks", label: "Вебхуки", icon: "◫", match: ["webhooks"] },
  { route: "audit", label: "Аудит-лог", icon: "▥", match: ["audit"] },
  { route: "system", label: "Система", icon: "◈", match: ["system"] },
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
        {NAV_ITEMS.map((item) => (
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
