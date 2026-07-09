import { createContext } from "preact";
import { useCallback, useContext, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";

interface ToastItem {
  id: number;
  message: string;
  variant: "info" | "error";
}

interface ToastContextValue {
  push: (message: string, variant?: "info" | "error") => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

let nextId = 1;

export function ToastProvider({ children }: { children: ComponentChildren }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  const push = useCallback((message: string, variant: "info" | "error" = "info") => {
    const id = nextId++;
    setToasts((prev) => [...prev, { id, message, variant }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 5000);
  }, []);

  return (
    <ToastContext.Provider value={{ push }}>
      {children}
      <div class="toast-stack">
        {toasts.map((t) => (
          <div key={t.id} class={`toast ${t.variant === "error" ? "toast-error" : ""}`}>
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
