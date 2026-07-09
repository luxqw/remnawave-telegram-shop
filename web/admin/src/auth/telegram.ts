import { api, setToken } from "../api/client";

// Minimal shape of the global injected by https://telegram.org/js/telegram-web-app.js.
interface TelegramWebApp {
  initData: string;
  ready(): void;
  expand(): void;
  setHeaderColor?(color: string): void;
  setBackgroundColor?(color: string): void;
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

export function initTelegramChrome() {
  const webApp = getTelegramWebApp();
  if (!webApp) return;
  webApp.ready();
  webApp.expand();
  webApp.setHeaderColor?.("#08080A");
  webApp.setBackgroundColor?.("#08080A");
}
