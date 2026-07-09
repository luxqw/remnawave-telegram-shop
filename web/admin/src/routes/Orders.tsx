import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { Purchase, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";

const STATUSES = ["", "new", "pending", "paid", "cancel"];
const TYPES = ["", "crypto", "yookasa", "telegram", "tribute"];

export function Orders() {
  const [page, setPage] = useState<Page<Purchase> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [status, setStatus] = useState("");
  const [invoiceType, setInvoiceType] = useState("");

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "25" });
    if (status) params.set("status", status);
    if (invoiceType) params.set("invoiceType", invoiceType);
    api.get<Page<Purchase>>(`/admin/api/orders?${params.toString()}`).then(setPage);
  }, [pageNum, status, invoiceType]);

  const exportUrl = () => {
    const params = new URLSearchParams();
    if (status) params.set("status", status);
    if (invoiceType) params.set("invoiceType", invoiceType);
    return `/admin/api/orders/export.csv?${params.toString()}`;
  };

  const columns: Column<Purchase>[] = [
    { header: "ID", render: (p) => <span class="mono">{p.id}</span> },
    { header: "Сумма", render: (p) => `${p.amount.toFixed(2)} ${p.currency}`, align: "right" },
    { header: "Мес.", render: (p) => p.month, align: "right" },
    { header: "Статус", render: (p) => <Badge variant={p.status === "paid" ? "success" : p.status === "cancel" ? "danger" : "neutral"}>{p.status}</Badge> },
    { header: "Тип", render: (p) => p.invoiceType },
    { header: "Создан", render: (p) => new Date(p.createdAt).toLocaleString("ru-RU") },
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
            <DataTable columns={columns} rows={page.items} keyFn={(p) => p.id} emptyMessage="Заказов не найдено" />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
    </div>
  );
}
