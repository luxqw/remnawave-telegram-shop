import { useEffect, useState } from "preact/hooks";

// A ~1kb hash router built on window.location.hash + useState. A full router library is
// overkill for 9 flat screens with no nested layouts beyond the sidebar shell.

export type Route =
  | { name: "dashboard" }
  | { name: "users" }
  | { name: "user-detail"; id: number }
  | { name: "orders" }
  | { name: "broadcast" }
  | { name: "referrals" }
  | { name: "webhooks" }
  | { name: "audit" }
  | { name: "activity" }
  | { name: "notifications" }
  | { name: "system" };

function parseHash(hash: string): Route {
  const path = hash.replace(/^#\/?/, "");
  const segments = path.split("/").filter(Boolean);

  if (segments.length === 0 || segments[0] === "dashboard") return { name: "dashboard" };
  if (segments[0] === "users" && segments[1]) {
    const id = Number(segments[1]);
    if (!Number.isNaN(id)) return { name: "user-detail", id };
  }
  if (segments[0] === "users") return { name: "users" };
  if (segments[0] === "orders") return { name: "orders" };
  if (segments[0] === "broadcast") return { name: "broadcast" };
  if (segments[0] === "referrals") return { name: "referrals" };
  if (segments[0] === "webhooks") return { name: "webhooks" };
  if (segments[0] === "audit") return { name: "audit" };
  if (segments[0] === "activity") return { name: "activity" };
  if (segments[0] === "notifications") return { name: "notifications" };
  if (segments[0] === "system") return { name: "system" };
  return { name: "dashboard" };
}

export function navigate(path: string) {
  window.location.hash = path.startsWith("/") ? path : `/${path}`;
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash));

  useEffect(() => {
    const onHashChange = () => setRoute(parseHash(window.location.hash));
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  return route;
}
