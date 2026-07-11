import { useEffect } from "preact/hooks";
import { api, setToken } from "../api/client";

// Minimal shape of the global injected by https://telegram.org/js/telegram-web-app.js.
interface TelegramWebAppBackButton {
  show(): void;
  hide(): void;
  onClick(cb: () => void): void;
  offClick(cb: () => void): void;
}

interface TelegramWebApp {
  initData: string;
  ready(): void;
  expand(): void;
  setHeaderColor?(color: string): void;
  setBackgroundColor?(color: string): void;
  BackButton?: TelegramWebAppBackButton;
  openTelegramLink?(url: string): void;
}

declare global {
  interface Window {
    Telegram?: { WebApp?: TelegramWebApp };
  }
}

interface LoginResponse {
  token: string;
  expiresAt: number;
  user: { id: number; firstName: string; lastName?: string; username?: string; photoUrl?: string };
}

export function getTelegramWebApp(): TelegramWebApp | null {
  return window.Telegram?.WebApp ?? null;
}

// login exchanges Telegram.WebApp.initData for a session token, storing it in sessionStorage.
// Throws if opened outside Telegram (no initData) or if the backend rejects the login.
export async function login(): Promise<LoginResponse> {
  const webApp = getTelegramWebApp();
  if (!webApp || !webApp.initData) {
    throw new Error(
      "Это приложение нужно открывать через кнопку «Веб-панель» в Telegram-боте.",
    );
  }
  const res = await api.post<LoginResponse>("/admin/api/auth/login", {
    initData: webApp.initData,
  });
  setToken(res.token);
  return res;
}

// The black-glass palette is this app's visual identity, not a system preference — deliberately
// NOT wired to Telegram.WebApp.themeParams/colorScheme, so the panel looks the same regardless of
// whether the admin's Telegram client is set to light or dark. setHeaderColor/setBackgroundColor
// below just match Telegram's own chrome to that fixed palette, they don't read anything from it.
export function initTelegramChrome() {
  const webApp = getTelegramWebApp();
  if (!webApp) return;
  webApp.ready();
  webApp.expand();
  webApp.setHeaderColor?.("#08080A");
  webApp.setBackgroundColor?.("#08080A");
}

// Opens a Telegram user's profile by username. Mini Apps run inside a sandboxed WebView that has
// no handler for the tg:// custom URL scheme (attempting to navigate one throws "unknown url
// scheme"), so tg://user?id= deep links don't work here — Telegram.WebApp.openTelegramLink() is
// the only officially supported in-app navigation, and it only accepts https://t.me/<username>
// links. Falls back to a plain new-tab open outside of Telegram (e.g. testing in a browser tab).
export function openTelegramProfile(username: string) {
  const url = `https://t.me/${username}`;
  const webApp = getTelegramWebApp();
  if (webApp?.openTelegramLink) {
    webApp.openTelegramLink(url);
  } else {
    window.open(url, "_blank", "noreferrer");
  }
}

// Shows Telegram's native chrome-level back button while `enabled` is true, wired to `onBack`.
// Purely additive to any in-page "← Back" link — the native control is unavailable outside a
// real Telegram client (e.g. a plain browser tab), so screens should keep an in-page fallback.
export function useTelegramBackButton(onBack: () => void, enabled: boolean) {
  useEffect(() => {
    const webApp = getTelegramWebApp();
    const backButton = webApp?.BackButton;
    if (!backButton || !enabled) return;
    backButton.onClick(onBack);
    backButton.show();
    return () => {
      backButton.offClick(onBack);
      backButton.hide();
    };
  }, [onBack, enabled]);
}
