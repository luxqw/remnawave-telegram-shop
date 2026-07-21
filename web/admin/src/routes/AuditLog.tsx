import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { AuditLogEntry, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { TelegramUserLink } from "../components/TelegramUserLink";
import { useDebounce } from "../lib/useDebounce";

export function AuditLog() {
  const [page, setPage] = useState<Page<AuditLogEntry> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [action, setAction] = useState("");
  const [outcome, setOutcome] = useState("");
  const [target, setTarget] = useState("");
  const debouncedAction = useDebounce(action, 300);
  const debouncedTarget = useDebounce(target, 300);

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "30" });
    if (debouncedAction) params.set("action", debouncedAction);
    if (outcome) params.set("outcome", outcome);
    if (debouncedTarget) params.set("target", debouncedTarget);
    api.get<Page<AuditLogEntry>>(`/admin/api/audit?${params.toString()}`).then(setPage);
  }, [pageNum, debouncedAction, outcome, debouncedTarget]);

  const columns: Column<AuditLogEntry>[] = [
    { header: "Дата", render: (e) => new Date(e.createdAt).toLocaleString("ru-RU") },
    { header: "Действие", render: (e) => e.action },
    { header: "Админ", render: (e) => <TelegramUserLink id={e.adminTelegramId} username={e.adminUsername} /> },
    { header: "Цель", render: (e) => <TelegramUserLink id={e.targetTelegramId} username={e.targetUsername} /> },
    { header: "Исход", render: (e) => <Badge variant={e.outcome === "success" ? "success" : "danger"}>{e.outcome}</Badge> },
    { header: "Источник", render: (e) => e.source },
    { header: "Ошибка", render: (e) => e.errorMessage ?? "—" },
  ];

  return (
    <div class="stack">
      <div class="row">
        <input class="input" placeholder="Действие (напр. topup)" value={action} onInput={(e) => { setAction((e.target as HTMLInputElement).value); setPageNum(1); }} style={{ maxWidth: 200 }} />
        <select class="select" value={outcome} onChange={(e) => { setOutcome((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          <option value="">Любой исход</option>
          <option value="success">success</option>
          <option value="failure">failure</option>
        </select>
        <input class="input" placeholder="Target Telegram ID" value={target} onInput={(e) => { setTarget((e.target as HTMLInputElement).value); setPageNum(1); }} style={{ maxWidth: 200 }} />
      </div>
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable columns={columns} rows={page.items} keyFn={(e) => e.id} emptyMessage="Записей не найдено" />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
    </div>
  );
}
