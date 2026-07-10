import { render } from "preact";
import { useEffect, useState } from "preact/hooks";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/components.css";

import { getToken, setUnauthorizedHandler } from "./api/client";
import { login, initTelegramChrome } from "./auth/telegram";
import { useRoute, type Route } from "./router";
import { Sidebar } from "./components/Sidebar";
import { Topbar } from "./components/Topbar";
import { ToastProvider } from "./components/Toast";

import { Dashboard } from "./routes/Dashboard";
import { Users } from "./routes/Users";
import { UserDetail } from "./routes/UserDetail";
import { Orders } from "./routes/Orders";
import { Broadcast } from "./routes/Broadcast";
import { Referrals } from "./routes/Referrals";
import { Webhooks } from "./routes/Webhooks";
import { AuditLog } from "./routes/AuditLog";
import { System } from "./routes/System";

const TITLES: Record<Route["name"], string> = {
  dashboard: "Дашборд",
  users: "Пользователи",
  "user-detail": "Пользователь",
  orders: "Заказы",
  broadcast: "Рассылка",
  referrals: "Рефералы",
  webhooks: "Вебхуки",
  audit: "Аудит-лог",
  system: "Система",
};

function RouteView({ route }: { route: Route }) {
  switch (route.name) {
    case "dashboard":
      return <Dashboard />;
    case "users":
      return <Users />;
    case "user-detail":
      return <UserDetail id={route.id} />;
    case "orders":
      return <Orders />;
    case "broadcast":
      return <Broadcast />;
    case "referrals":
      return <Referrals />;
    case "webhooks":
      return <Webhooks />;
    case "audit":
      return <AuditLog />;
    case "system":
      return <System />;
  }
}

type AuthState = { status: "loading" } | { status: "error"; message: string } | { status: "ready"; adminId: number };

function App() {
  const [auth, setAuth] = useState<AuthState>({ status: "loading" });
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const route = useRoute();

  // Auto-close the off-canvas drawer whenever the route changes (e.g. after tapping a nav item).
  useEffect(() => setMobileNavOpen(false), [route.name]);

  useEffect(() => {
    initTelegramChrome();
    setUnauthorizedHandler(() => setAuth({ status: "error", message: "Сессия истекла. Перезайдите." }));

    (async () => {
      if (getToken()) {
        // Already have a session from a previous load in this tab — trust it until an API call
        // proves otherwise (the client's 401 handler will flip us back to the error state).
        setAuth({ status: "ready", adminId: 0 });
        return;
      }
      try {
        const res = await login();
        setAuth({ status: "ready", adminId: res.user.id });
      } catch (err) {
        setAuth({ status: "error", message: err instanceof Error ? err.message : "Ошибка входа" });
      }
    })();
  }, []);

  if (auth.status === "loading") {
    return <div class="centered-loader">Вход…</div>;
  }
  if (auth.status === "error") {
    return (
      <div class="centered-loader stack" style={{ flexDirection: "column", alignItems: "center" }}>
        <p>{auth.message}</p>
      </div>
    );
  }

  return (
    <div class="app-shell">
      <Sidebar current={route} open={mobileNavOpen} onNavigate={() => setMobileNavOpen(false)} />
      <div class="main-column">
        <Topbar
          adminId={auth.adminId || null}
          title={TITLES[route.name]}
          onMenuClick={() => setMobileNavOpen((o) => !o)}
        />
        <div class="page-content">
          <RouteView route={route} />
        </div>
      </div>
    </div>
  );
}

render(
  <ToastProvider>
    <App />
  </ToastProvider>,
  document.getElementById("app")!,
);
