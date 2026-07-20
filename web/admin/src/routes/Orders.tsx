import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { Purchase, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { TelegramUserLink } from "../components/TelegramUserLink";
import { DetailModal } from "../components/DetailModal";
import { formatMoney } from "../lib/format";

const STATUSES = ["", "new", "pending", "paid", "cancel"];
const TYPES = ["", "crypto", "yookasa", "telegram", "tribute", "rollypay"];

export function Orders() {
  const [page, setPage] = useState<Page<Purchase> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [status, setStatus] = useState("");
  const [invoiceType, setInvoiceType] = useState("");
  const [detail, setDetail] = useState<Purchase | null>(null);

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "25" });
    if (status) params.set("status", status);
    if (invoiceType) params.set("invoiceType", invoiceType);
    api.get<Page<Purchase>>(`/admin/api/orders?${params.toString()}`).then(setPage);
  }, [pageNum, status, invoiceType]);

  const openDetail = (p: Purchase) => {
    api.get<Purchase>(`/admin/api/orders/${p.id}`).then(setDetail).catch(() => setDetail(p));
  };

  const exportUrl = () => {
    const params = new URLSearchParams();
    if (status) params.set("status", status);
    if (invoiceType) params.set("invoiceType", invoiceType);
    return `/admin/api/orders/export.csv?${params.toString()}`;
  };

  const columns: Column<Purchase>[] = [
    { header: "ID", render: (p) => <span class="mono">{p.id}</span>, sortKey: "id", sortValue: (p) => p.id },
    {
      header: "Пользователь",
      render: (p) => <TelegramUserLink id={p.telegramId} username={p.username} />,
      sortKey: "telegramId",
      sortValue: (p) => p.telegramId ?? null,
    },
    {
      header: "Сумма",
      render: (p) => formatMoney(p.amount, p.currency),
      align: "right",
      sortKey: "amount",
      sortValue: (p) => p.amount,
    },
    { header: "Мес.", render: (p) => p.month, align: "right", sortKey: "month", sortValue: (p) => p.month },
    {
      header: "Статус",
      render: (p) => <Badge variant={p.status === "paid" ? "success" : p.status === "cancel" ? "danger" : "neutral"}>{p.status}</Badge>,
      sortKey: "status",
      sortValue: (p) => p.status,
    },
    { header: "Тип", render: (p) => p.invoiceType, sortKey: "invoiceType", sortValue: (p) => p.invoiceType },
    {
      header: "Создан",
      render: (p) => new Date(p.createdAt).toLocaleString("ru-RU"),
      sortKey: "createdAt",
      sortValue: (p) => new Date(p.createdAt).getTime(),
    },
  ];

  return (
    <div class="stack">
      <div class="row">
        <select class="select" value={status} onChange={(e) => { setStatus((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {STATUSES.map((s) => <option key={s} value={s}>{s || "Все статусы"}</option>)}
        </select>
        <select class="select" value={invoiceType} onChange={(e) => { setInvoiceType((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {TYPES.map((t) => <option key={t} value={t}>{t || "Все типы"}</option>)}
        </select>
        <div class="spacer" />
        <a class="btn btn-sm" href={exportUrl()} target="_blank" rel="noreferrer">Экспорт CSV</a>
      </div>
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable columns={columns} rows={page.items} keyFn={(p) => p.id} onRowClick={openDetail} emptyMessage="Заказов не найдено" />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
      <DetailModal open={detail !== null} title={`Заказ #${detail?.id ?? ""}`} onClose={() => setDetail(null)} body={detail && (
        <div class="stack" style={{ gap: 6 }}>
          <div class="data-card-row"><span class="data-card-label">ID</span><span class="data-card-value mono">{detail.id}</span></div>
          <div class="data-card-row"><span class="data-card-label">Пользователь</span><span class="data-card-value"><TelegramUserLink id={detail.telegramId} username={detail.username} /></span></div>
          <div class="data-card-row"><span class="data-card-label">Сумма</span><span class="data-card-value">{formatMoney(detail.amount, detail.currency)}</span></div>
          <div class="data-card-row"><span class="data-card-label">Месяцев</span><span class="data-card-value">{detail.month}</span></div>
          <div class="data-card-row"><span class="data-card-label">Статус</span><span class="data-card-value">{detail.status}</span></div>
          <div class="data-card-row"><span class="data-card-label">Тип оплаты</span><span class="data-card-value">{detail.invoiceType}</span></div>
          <div class="data-card-row"><span class="data-card-label">Создан</span><span class="data-card-value">{new Date(detail.createdAt).toLocaleString("ru-RU")}</span></div>
          <div class="data-card-row"><span class="data-card-label">Оплачен</span><span class="data-card-value">{detail.paidAt ? new Date(detail.paidAt).toLocaleString("ru-RU") : "—"}</span></div>
          <div class="data-card-row"><span class="data-card-label">Истекает</span><span class="data-card-value">{detail.expireAt ? new Date(detail.expireAt).toLocaleString("ru-RU") : "—"}</span></div>
        </div>
      )} />
    </div>
  );
}
