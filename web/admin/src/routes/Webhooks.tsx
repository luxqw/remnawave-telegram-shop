import { useEffect, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { WebhookInboxEntry, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { useToast } from "../components/Toast";

const STATUSES = ["", "pending", "processed", "failed"];

export function Webhooks() {
  const toast = useToast();
  const [page, setPage] = useState<Page<WebhookInboxEntry> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [status, setStatus] = useState("failed");
  const [reloadTick, setReloadTick] = useState(0);

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "25" });
    if (status) params.set("status", status);
    api.get<Page<WebhookInboxEntry>>(`/admin/api/webhooks?${params.toString()}`).then(setPage);
  }, [pageNum, status, reloadTick]);

  const retry = async (id: number) => {
    try {
      await api.post(`/admin/api/webhooks/${id}/retry`);
      toast.push(`Вебхук #${id} переотправлен`);
      setReloadTick((t) => t + 1);
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка ретрая", "error");
    }
  };

  const columns: Column<WebhookInboxEntry>[] = [
    { header: "ID", render: (w) => <span class="mono">{w.id}</span> },
    { header: "Событие", render: (w) => w.eventType || "—" },
    { header: "Статус", render: (w) => <Badge variant={w.status === "processed" ? "success" : w.status === "failed" ? "danger" : "neutral"}>{w.status}</Badge> },
    { header: "Попытки", render: (w) => w.attempts, align: "right" },
    { header: "Создан", render: (w) => new Date(w.createdAt).toLocaleString("ru-RU") },
    {
      header: "",
      render: (w) =>
        w.status === "failed" ? (
          <button class="btn btn-sm" onClick={(e) => { e.stopPropagation(); retry(w.id); }}>
            Повторить
          </button>
        ) : null,
    },
  ];

  return (
    <div class="stack">
      <div class="row">
        <select class="select" value={status} onChange={(e) => { setStatus((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {STATUSES.map((s) => <option key={s} value={s}>{s || "Все статусы"}</option>)}
        </select>
      </div>
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable columns={columns} rows={page.items} keyFn={(w) => w.id} emptyMessage="Вебхуков не найдено" />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
    </div>
  );
}
