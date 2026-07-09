import { useRef, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { FixStrategyPreview, FixStrategyJobStatus } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { ConfirmModal } from "../components/ConfirmModal";
import { useToast } from "../components/Toast";

export function System() {
  const toast = useToast();
  const [syncing, setSyncing] = useState(false);
  const [preview, setPreview] = useState<FixStrategyPreview | null>(null);
  const [confirmApplyOpen, setConfirmApplyOpen] = useState(false);
  const [job, setJob] = useState<FixStrategyJobStatus | null>(null);
  const pollRef = useRef<number | null>(null);

  const runSync = async () => {
    setSyncing(true);
    try {
      await api.post("/admin/api/system/sync");
      toast.push("Синхронизация запущена");
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка", "error");
    } finally {
      setSyncing(false);
    }
  };

  const loadPreview = async () => {
    try {
      const res = await api.post<FixStrategyPreview>("/admin/api/system/fix-traffic-strategy/preview");
      setPreview(res);
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка", "error");
    }
  };

  const apply = async () => {
    try {
      const res = await api.post<{ jobId: string }>("/admin/api/system/fix-traffic-strategy/apply");
      setConfirmApplyOpen(false);
      toast.push("Применение запущено в фоне");
      pollRef.current = window.setInterval(async () => {
        const status = await api.get<FixStrategyJobStatus>(`/admin/api/system/fix-traffic-strategy/status/${res.jobId}`);
        setJob(status);
        if (status.done && pollRef.current) {
          clearInterval(pollRef.current);
          pollRef.current = null;
        }
      }, 1000);
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка запуска", "error");
    }
  };

  return (
    <div class="stack">
      <GlassCard>
        <div class="stat-tile-label" style={{ marginBottom: 10 }}>Синхронизация с Remnawave</div>
        <p class="page-subtitle" style={{ marginBottom: 12 }}>
          Безопасная операция — подтверждение не требуется.
        </p>
        <button class="btn" onClick={runSync} disabled={syncing}>
          {syncing ? "Запуск…" : "Синхронизировать"}
        </button>
      </GlassCard>

      <GlassCard>
        <div class="stat-tile-label" style={{ marginBottom: 10 }}>Стратегия сброса трафика</div>
        <div class="row" style={{ marginBottom: 12 }}>
          <button class="btn btn-sm" onClick={loadPreview}>Предпросмотр</button>
          {preview && preview.willUpdate > 0 && (
            <button class="btn btn-sm btn-danger" onClick={() => setConfirmApplyOpen(true)}>
              Применить ({preview.willUpdate})
            </button>
          )}
        </div>
        {preview && (
          <div class="stack">
            <div>Всего пользователей: <span class="mono">{preview.totalCustomers}</span></div>
            <div>Целевая стратегия: <span class="mono">{preview.targetStrategy}</span></div>
            <div>Будет обновлено: <span class="mono">{preview.willUpdate}</span></div>
            <div>Не найдено в панели: <span class="mono">{preview.notFound}</span></div>
            <div class="row" style={{ gap: 16, flexWrap: "wrap" }}>
              {Object.entries(preview.strategyCounts).map(([strategy, count]) => (
                <span key={strategy}>{strategy}: <span class="mono">{count}</span></span>
              ))}
            </div>
          </div>
        )}
        {job && (
          <div class="stack" style={{ marginTop: 16 }}>
            <div class="stat-tile-label">Прогресс {job.done ? "(завершено)" : "…"}</div>
            {job.error && <div class="page-subtitle">Ошибка: {job.error}</div>}
            <div class="row" style={{ gap: 24 }}>
              <span>Обработано: <span class="mono">{job.processed}</span> / {job.total}</span>
              <span>Обновлено: <span class="mono">{job.updated}</span></span>
              <span>Ошибок: <span class="mono">{job.errored}</span></span>
            </div>
          </div>
        )}
      </GlassCard>

      <ConfirmModal
        open={confirmApplyOpen}
        title="Применить массовое обновление стратегии?"
        body={`Будет обновлено ${preview?.willUpdate ?? 0} пользователей в Remnawave. Это bulk-операция по всем клиентам.`}
        confirmLabel="Применить"
        danger
        onCancel={() => setConfirmApplyOpen(false)}
        onConfirm={apply}
      />
    </div>
  );
}
