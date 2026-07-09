import { useEffect, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { UserDetail as UserDetailDTO, Purchase, AuditLogEntry, Referral, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { Badge } from "../components/Badge";
import { ConfirmModal } from "../components/ConfirmModal";
import { DataTable, type Column } from "../components/DataTable";
import { useToast } from "../components/Toast";
import { navigate } from "../router";

type PendingAction =
  | { kind: "status"; status: "ACTIVE" | "DISABLED" }
  | { kind: "reset-devices" }
  | { kind: "reset-traffic" }
  | { kind: "topup"; gb: number; previewLimitGb: number | null }
  | { kind: "extend"; days: number }
  | { kind: "trial"; isTrial: boolean };

export function UserDetail(props: { id: number }) {
  const toast = useToast();
  const [detail, setDetail] = useState<UserDetailDTO | null>(null);
  const [orders, setOrders] = useState<Page<Purchase> | null>(null);
  const [audit, setAudit] = useState<AuditLogEntry[] | null>(null);
  const [referrals, setReferrals] = useState<Referral[] | null>(null);
  const [tab, setTab] = useState<"orders" | "audit" | "referrals">("orders");
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [gbInput, setGbInput] = useState("10");
  const [daysInput, setDaysInput] = useState("30");
  const [reloadTick, setReloadTick] = useState(0);

  const load = () => {
    api.get<UserDetailDTO>(`/admin/api/users/${props.id}`).then(setDetail).catch(() => setDetail(null));
  };

  useEffect(load, [props.id, reloadTick]);

  useEffect(() => {
    if (tab === "orders") api.get<Page<Purchase>>(`/admin/api/users/${props.id}/orders`).then(setOrders);
    if (tab === "audit") api.get<AuditLogEntry[]>(`/admin/api/users/${props.id}/audit`).then(setAudit);
    if (tab === "referrals") api.get<Referral[]>(`/admin/api/users/${props.id}/referrals`).then(setReferrals);
  }, [tab, props.id, reloadTick]);

  const refreshAll = () => setReloadTick((t) => t + 1);

  const openTopupConfirm = async () => {
    const gb = parseInt(gbInput, 10);
    if (!gb) {
      toast.push("Введите ненулевое число ГБ", "error");
      return;
    }
    try {
      const preview = await api.post<{ newLimitGb: number }>(`/admin/api/users/${props.id}/topup/preview`, { gb });
      setPending({ kind: "topup", gb, previewLimitGb: preview.newLimitGb });
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка", "error");
    }
  };

  const execute = async () => {
    if (!pending) return;
    try {
      switch (pending.kind) {
        case "status":
          await api.post(`/admin/api/users/${props.id}/status`, { status: pending.status });
          toast.push(pending.status === "ACTIVE" ? "Пользователь включён" : "Пользователь отключён");
          break;
        case "reset-devices":
          await api.post(`/admin/api/users/${props.id}/reset-devices`);
          toast.push("Устройства сброшены");
          break;
        case "reset-traffic":
          await api.post(`/admin/api/users/${props.id}/reset-traffic`);
          toast.push("Трафик сброшен");
          break;
        case "topup":
          await api.post(`/admin/api/users/${props.id}/topup`, { gb: pending.gb });
          toast.push("Топап применён");
          break;
        case "extend":
          await api.post(`/admin/api/users/${props.id}/extend`, { days: pending.days });
          toast.push("Подписка продлена");
          break;
        case "trial":
          await api.post(`/admin/api/users/${props.id}/trial`, { isTrial: pending.isTrial });
          toast.push("Статус триала изменён");
          break;
      }
      setPending(null);
      refreshAll();
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка выполнения", "error");
    }
  };

  if (detail === null) return <div class="shimmer" style={{ height: 240 }} />;

  const { customer, remnawave, remnawaveError } = detail;

  const orderColumns: Column<Purchase>[] = [
    { header: "ID", render: (p) => <span class="mono">{p.id}</span> },
    { header: "Сумма", render: (p) => `${p.amount.toFixed(2)} ${p.currency}`, align: "right" },
    { header: "Статус", render: (p) => <Badge variant={p.status === "paid" ? "success" : "neutral"}>{p.status}</Badge> },
    { header: "Тип", render: (p) => p.invoiceType },
    { header: "Создан", render: (p) => new Date(p.createdAt).toLocaleDateString("ru-RU") },
  ];

  const auditColumns: Column<AuditLogEntry>[] = [
    { header: "Дата", render: (e) => new Date(e.createdAt).toLocaleString("ru-RU") },
    { header: "Действие", render: (e) => e.action },
    { header: "Исход", render: (e) => <Badge variant={e.outcome === "success" ? "success" : "danger"}>{e.outcome}</Badge> },
    { header: "Источник", render: (e) => e.source },
    { header: "Параметр", render: (e) => (e.paramInt !== null ? <span class="mono">{e.paramInt}</span> : "—") },
  ];

  const referralColumns: Column<Referral>[] = [
    { header: "Приглашённый", render: (r) => <span class="mono">{r.refereeId}</span> },
    { header: "Дата", render: (r) => new Date(r.usedAt).toLocaleDateString("ru-RU") },
    { header: "Бонус", render: (r) => (r.bonusGranted ? <Badge variant="success">Начислен</Badge> : <Badge variant="neutral">Ожидание</Badge>) },
  ];

  return (
    <div class="stack">
      <button class="btn btn-ghost btn-sm" onClick={() => navigate("users")}>← К списку</button>

      <div class="bento-grid">
        <div class="bento-main">
          <GlassCard>
            <div class="row" style={{ justifyContent: "space-between" }}>
              <div>
                <div class="page-title" style={{ fontSize: 18 }}>
                  <span class="mono">{customer.telegramId}</span>
                </div>
                <div class="page-subtitle">Язык: {customer.language} · Регистрация: {new Date(customer.createdAt).toLocaleDateString("ru-RU")}</div>
              </div>
              {customer.isTrial ? <Badge variant="neutral">Триал</Badge> : <Badge variant="success">Платный</Badge>}
            </div>

            <div class="stack" style={{ marginTop: 16 }}>
              <div>
                <span class="stat-tile-label">Подписка (в боте): </span>
                {customer.expireAt ? new Date(customer.expireAt).toLocaleString("ru-RU") : "отсутствует"}
              </div>
              {remnawaveError && <div class="page-subtitle">⚠ Remnawave: {remnawaveError}</div>}
              {remnawave && (
                <div class="row" style={{ gap: 24, flexWrap: "wrap" }}>
                  <span>Статус RW: <Badge variant={remnawave.status === "ACTIVE" ? "success" : "danger"}>{remnawave.status}</Badge></span>
                  <span>Лимит: <span class="mono">{remnawave.trafficLimitGb} GB</span></span>
                  <span>Стратегия: <span class="mono">{remnawave.trafficLimitStrategy}</span></span>
                  <span>RW истекает: {new Date(remnawave.expireAt).toLocaleDateString("ru-RU")}</span>
                </div>
              )}
            </div>
          </GlassCard>

          <GlassCard style={{ marginTop: 16 }}>
            <div class="row" style={{ marginBottom: 12 }}>
              <button class={`btn btn-sm ${tab === "orders" ? "btn-primary" : ""}`} onClick={() => setTab("orders")}>Заказы</button>
              <button class={`btn btn-sm ${tab === "audit" ? "btn-primary" : ""}`} onClick={() => setTab("audit")}>Аудит</button>
              <button class={`btn btn-sm ${tab === "referrals" ? "btn-primary" : ""}`} onClick={() => setTab("referrals")}>Рефералы</button>
            </div>
            {tab === "orders" && <DataTable columns={orderColumns} rows={orders?.items ?? []} keyFn={(p) => p.id} emptyMessage="Заказов нет" />}
            {tab === "audit" && <DataTable columns={auditColumns} rows={audit ?? []} keyFn={(e) => e.id} emptyMessage="Записей аудита нет" />}
            {tab === "referrals" && <DataTable columns={referralColumns} rows={referrals ?? []} keyFn={(r) => r.id} emptyMessage="Рефералов нет" />}
          </GlassCard>
        </div>

        <div class="bento-side">
          <GlassCard>
            <div class="stat-tile-label" style={{ marginBottom: 10 }}>Действия</div>
            <div class="stack">
              <div class="row">
                <button class="btn btn-sm" onClick={() => setPending({ kind: "status", status: "ACTIVE" })}>Включить</button>
                <button class="btn btn-sm btn-danger" onClick={() => setPending({ kind: "status", status: "DISABLED" })}>Отключить</button>
              </div>
              <button class="btn btn-sm btn-danger" onClick={() => setPending({ kind: "reset-devices" })}>Сбросить устройства</button>
              <button class="btn btn-sm btn-danger" onClick={() => setPending({ kind: "reset-traffic" })}>Сбросить трафик</button>

              <div class="field">
                <label class="field-label">Топап ГБ (может быть отрицательным)</label>
                <div class="row">
                  <input class="input" style={{ width: 90 }} value={gbInput} onInput={(e) => setGbInput((e.target as HTMLInputElement).value)} />
                  <button class="btn btn-sm" onClick={openTopupConfirm}>Применить</button>
                </div>
              </div>

              <div class="field">
                <label class="field-label">Продлить на дней</label>
                <div class="row">
                  <input class="input" style={{ width: 90 }} value={daysInput} onInput={(e) => setDaysInput((e.target as HTMLInputElement).value)} />
                  <button
                    class="btn btn-sm"
                    onClick={() => {
                      const days = parseInt(daysInput, 10);
                      if (days > 0) setPending({ kind: "extend", days });
                    }}
                  >
                    Продлить
                  </button>
                </div>
              </div>

              <button
                class="btn btn-sm"
                onClick={() => setPending({ kind: "trial", isTrial: !customer.isTrial })}
              >
                {customer.isTrial ? "Снять триал" : "Сделать триал"}
              </button>
            </div>
          </GlassCard>
        </div>
      </div>

      <ConfirmModal
        open={pending !== null}
        title={confirmTitle(pending)}
        body={confirmBody(pending)}
        danger={isDanger(pending)}
        onCancel={() => setPending(null)}
        onConfirm={execute}
      />
    </div>
  );
}

function confirmTitle(pending: PendingAction | null): string {
  if (!pending) return "";
  switch (pending.kind) {
    case "status":
      return pending.status === "ACTIVE" ? "Включить пользователя?" : "Отключить пользователя?";
    case "reset-devices":
      return "Сбросить устройства?";
    case "reset-traffic":
      return "Сбросить трафик?";
    case "topup":
      return "Применить топап?";
    case "extend":
      return "Продлить подписку?";
    case "trial":
      return pending.isTrial ? "Сделать пользователя триальным?" : "Снять триальный статус?";
  }
}

function confirmBody(pending: PendingAction | null) {
  if (!pending) return null;
  switch (pending.kind) {
    case "topup":
      return `${pending.gb > 0 ? "+" : ""}${pending.gb} GB. Новый лимит: ${pending.previewLimitGb ?? "?"} GB.`;
    case "extend":
      return `Подписка будет продлена на ${pending.days} дн.`;
    default:
      return "Действие будет немедленно применено и записано в аудит-лог.";
  }
}

function isDanger(pending: PendingAction | null): boolean {
  if (!pending) return false;
  return (
    pending.kind === "reset-devices" ||
    pending.kind === "reset-traffic" ||
    (pending.kind === "status" && pending.status === "DISABLED") ||
    (pending.kind === "topup" && pending.gb < 0)
  );
}
