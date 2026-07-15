import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "../api/client";
import type { Customer, Page } from "../api/types";
import { navigate } from "../router";

const DEBOUNCE_MS = 250;
const RESULT_LIMIT = 8;

// OPEN_EVENT lets the Topbar's "⌘K" hint button open the palette without any shared
// context/state — CommandPalette is a singleton mounted once at the app root (main.tsx), so a
// plain window CustomEvent is enough and avoids threading open-state through props.
const OPEN_EVENT = "admin:open-command-palette";

export function openCommandPalette() {
  window.dispatchEvent(new CustomEvent(OPEN_EVENT));
}

// CommandPalette is a Cmd+K / Ctrl+K global quick-search: type a telegram ID or username
// fragment, get up to 8 matching customers, navigate to the selected one. Mounted once at the
// app root, not per-page.
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Customer[]>([]);
  const [selected, setSelected] = useState(0);
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const close = () => setOpen(false);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    const onOpenEvent = () => setOpen(true);
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener(OPEN_EVENT, onOpenEvent);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener(OPEN_EVENT, onOpenEvent);
    };
  }, []);

  useEffect(() => {
    if (open) {
      // Deferred so the input exists in the DOM before we try to focus it.
      setTimeout(() => inputRef.current?.focus(), 0);
    } else {
      setQuery("");
      setResults([]);
      setSelected(0);
    }
  }, [open]);

  useEffect(() => {
    if (!open || !query.trim()) {
      setResults([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    const handle = setTimeout(() => {
      const params = new URLSearchParams({ search: query.trim(), limit: String(RESULT_LIMIT) });
      api
        .get<Page<Customer>>(`/admin/api/users?${params.toString()}`)
        .then((page) => {
          setResults(page.items);
          setSelected(0);
        })
        .catch(() => setResults([]))
        .finally(() => setLoading(false));
    }, DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [query, open]);

  const select = (c: Customer) => {
    navigate(`users/${c.telegramId}`);
    close();
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, results.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (results[selected]) select(results[selected]);
    }
  };

  if (!open) return null;

  return (
    <div class="modal-backdrop" onClick={close}>
      <div class="modal command-palette" onClick={(e) => e.stopPropagation()}>
        <input
          ref={inputRef}
          class="input command-palette-input"
          placeholder="Поиск пользователя по ID или username…"
          value={query}
          onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
          onKeyDown={onKeyDown}
        />
        <div class="command-palette-results">
          {loading && (
            <div class="page-subtitle" style={{ padding: "10px 4px" }}>
              Поиск…
            </div>
          )}
          {!loading && query.trim() !== "" && results.length === 0 && (
            <div class="page-subtitle" style={{ padding: "10px 4px" }}>
              Ничего не найдено
            </div>
          )}
          {!loading &&
            results.map((c, i) => (
              <div
                key={c.id}
                class={`command-palette-item ${i === selected ? "active" : ""}`}
                onMouseEnter={() => setSelected(i)}
                onClick={() => select(c)}
              >
                <span class="mono">{c.telegramId}</span>
                <span class="command-palette-item-username">
                  {c.username ? `@${c.username}` : "без username"}
                </span>
              </div>
            ))}
        </div>
      </div>
    </div>
  );
}
