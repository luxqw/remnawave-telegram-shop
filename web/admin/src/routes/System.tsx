import { useEffect, useRef, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { FixStrategyPreview, FixStrategyJobStatus, RuntimeSettings } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { ConfirmModal } from "../components/ConfirmModal";
import { useToast } from "../components/Toast";

// SETTING_LABELS mirrors config.RuntimeSettingKeys — kept as a display label map here rather than
// derived from the API response so field order and Russian labels stay stable regardless of Go
// map iteration order.
const SETTING_LABELS: Record<string, string> = {
  PRICE_1: "Цена: 1 месяц (₽)",
  PRICE_3: "Цена: 3 месяца (₽)",
  PRICE_6: "Цена: 6 месяцев (₽)",
  PRICE_12: "Цена: 12 месяцев (₽)",
  DEVICE_SLOT_PRICE_RUB: "Цена слота устройства (₽/мес)",
};

export function System() {
  const toast = useToast();
  const [syncing, setSyncing] = useState(false);
  const [preview, setPreview] = useState<FixStrategyPreview | null>(null);
  const [confirmApplyOpen, setConfirmApplyOpen] = useState(false);
  const [job, setJob] = useState<FixStrategyJobStatus | null>(null);
  const pollRef = useRef<number | null>(null);
  const [settings, setSettings] = useState<RuntimeSettings | null>(null);
  const [settingsInput, setSettingsInput] = useState<RuntimeSettings>({});
  const [savingSettings, setSavingSettings] = useState(false);

  useEffect(() => {
    api.get<RuntimeSettings>("/admin/api/system/settings").then((s) => {
      setSettings(s);
      setSettingsInput(s);
    });
  }, []);

  const saveSettings = async () => {
    if (!settings) return;
    const changed: RuntimeSettings = {};
    for (const key of Object.keys(SETTING_LABELS)) {
      if (settingsInput[key] !== undefined && settingsInput[key] !== settings[key]) {
        changed[key] = settingsInput[key];
      }
    }
    if (Object.keys(changed).length === 0) {
      toast.push("Нет изменений");
      return;
    }
    setSavingSettings(true);
    try {
      const updated = await api.patch<RuntimeSettings>("/admin/api/system/settings", changed);
      setSettings(updated);
      setSettingsInput(updated);
      toast.push("Настройки применены без перезапуска");
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка сохранения", "error");
    } finally {
      setSavingSettings(false);
    }
  };

  useEffect(() => () => {
    if (pollRef.current) clearInterval(pollRef.current);
  }, []);

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
        <div class="stat-tile-label" style={{ marginBottom: 10 }}>Настройки (без перезапуска)</div>
        <p class="page-subtitle" style={{ marginBottom: 12 }}>
          Изменения применяются сразу и переживают деплой — хранятся в БД поверх .env.
        </p>
        {settings === null ? (
          <div class="page-subtitle">Загрузка…</div>
        ) : (
          <div class="stack">
            {Object.entries(SETTING_LABELS).map(([key, label]) => (
              <div class="field" key={key}>
                <label class="field-label">{label}</label>
                <input
                  class="input input-compact"
                  value={settingsInput[key] ?? ""}
                  onInput={(e) => setSettingsInput((prev) => ({ ...prev, [key]: (e.target as HTMLInputElement).value }))}
                />
              </div>
            ))}
            <button class="btn btn-sm" onClick={saveSettings} disabled={savingSettings}>
              {savingSettings ? "Сохранение…" : "Сохранить"}
            </button>
          </div>
        )}
      </GlassCard>

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
