import { useEffect, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { WebhookInboxEntry, WebhookInboxDetail, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { useToast } from "../components/Toast";
import { DetailModal } from "../components/DetailModal";

const STATUSES = ["", "pending", "processed", "failed"];
const PROVIDERS = ["", "tribute", "rollypay"];

export function Webhooks() {
  const toast = useToast();
  const [page, setPage] = useState<Page<WebhookInboxEntry> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [status, setStatus] = useState("failed");
  const [provider, setProvider] = useState("");
  const [reloadTick, setReloadTick] = useState(0);
  const [detail, setDetail] = useState<WebhookInboxDetail | null>(null);

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "25" });
    if (status) params.set("status", status);
    if (provider) params.set("provider", provider);
    api.get<Page<WebhookInboxEntry>>(`/admin/api/webhooks?${params.toString()}`).then(setPage);
  }, [pageNum, status, provider, reloadTick]);

  const openDetail = (w: WebhookInboxEntry) => {
    api.get<WebhookInboxDetail>(`/admin/api/webhooks/${w.id}`).then(setDetail);
  };

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
    { header: "ID", render: (w) => <span class="mono">{w.id}</span>, sortKey: "id", sortValue: (w) => w.id },
    { header: "Событие", render: (w) => w.eventType || "—", sortKey: "eventType", sortValue: (w) => w.eventType || null },
    { header: "Провайдер", render: (w) => w.provider, sortKey: "provider", sortValue: (w) => w.provider },
    {
      header: "Статус",
      render: (w) => <Badge variant={w.status === "processed" ? "success" : w.status === "failed" ? "danger" : "neutral"}>{w.status}</Badge>,
      sortKey: "status",
      sortValue: (w) => w.status,
    },
    { header: "Попытки", render: (w) => w.attempts, align: "right", sortKey: "attempts", sortValue: (w) => w.attempts },
    {
      header: "Создан",
      render: (w) => new Date(w.createdAt).toLocaleString("ru-RU"),
      sortKey: "createdAt",
      sortValue: (w) => new Date(w.createdAt).getTime(),
    },
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
        <select class="select" value={provider} onChange={(e) => { setProvider((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {PROVIDERS.map((p) => <option key={p} value={p}>{p || "Все провайдеры"}</option>)}
        </select>
      </div>
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable columns={columns} rows={page.items} keyFn={(w) => w.id} onRowClick={openDetail} emptyMessage="Вебхуков не найдено" />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
      <DetailModal open={detail !== null} title={`Вебхук #${detail?.id ?? ""}`} onClose={() => setDetail(null)} body={detail && (
        <div class="stack" style={{ gap: 6 }}>
          <div class="data-card-row"><span class="data-card-label">Событие</span><span class="data-card-value">{detail.eventType || "—"}</span></div>
          <div class="data-card-row"><span class="data-card-label">Провайдер</span><span class="data-card-value">{detail.provider}</span></div>
          <div class="data-card-row"><span class="data-card-label">Статус</span><span class="data-card-value">{detail.status}</span></div>
          <div class="data-card-row"><span class="data-card-label">Попытки</span><span class="data-card-value">{detail.attempts}</span></div>
          <div class="data-card-row"><span class="data-card-label">Создан</span><span class="data-card-value">{new Date(detail.createdAt).toLocaleString("ru-RU")}</span></div>
          <div class="data-card-row"><span class="data-card-label">Обработан</span><span class="data-card-value">{detail.processedAt ? new Date(detail.processedAt).toLocaleString("ru-RU") : "—"}</span></div>
          <div class="data-card-row"><span class="data-card-label">Ошибка</span><span class="data-card-value">{detail.errorMsg ?? "—"}</span></div>
          <div class="data-card-row" style={{ flexDirection: "column", alignItems: "stretch", gap: 4 }}>
            <span class="data-card-label">Payload</span>
            <pre class="mono" style={{ whiteSpace: "pre-wrap", wordBreak: "break-all", fontSize: 11, maxHeight: 240, overflow: "auto", margin: 0 }}>{detail.payload}</pre>
          </div>
        </div>
      )} />
    </div>
  );
}
