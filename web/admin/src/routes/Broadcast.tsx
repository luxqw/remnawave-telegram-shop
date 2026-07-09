import { useEffect, useRef, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { BroadcastProgress } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { ConfirmModal } from "../components/ConfirmModal";
import { useToast } from "../components/Toast";

const SEGMENTS = [
  { value: "active", label: "Активные подписчики" },
  { value: "expired", label: "Истёкшие" },
  { value: "inactive", label: "Неактивные" },
  { value: "new", label: "Не покупавшие" },
  { value: "all", label: "Все" },
];

export function Broadcast() {
  const toast = useToast();
  const [text, setText] = useState("");
  const [segment, setSegment] = useState("active");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [previewCount, setPreviewCount] = useState<number | null>(null);
  const [job, setJob] = useState<BroadcastProgress | null>(null);
  const pollRef = useRef<number | null>(null);

  useEffect(() => () => {
    if (pollRef.current) clearInterval(pollRef.current);
  }, []);

  const openConfirm = async () => {
    if (!text.trim()) {
      toast.push("Введите текст рассылки", "error");
      return;
    }
    try {
      const res = await api.post<{ recipientCount: number }>("/admin/api/broadcast/preview", { segment });
      setPreviewCount(res.recipientCount);
      setConfirmOpen(true);
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка", "error");
    }
  };

  const send = async () => {
    try {
      const res = await api.post<{ jobId: string }>("/admin/api/broadcast/send", { text, segment });
      setConfirmOpen(false);
      toast.push("Рассылка запущена");
      pollRef.current = window.setInterval(async () => {
        const status = await api.get<BroadcastProgress>(`/admin/api/broadcast/status/${res.jobId}`);
        setJob(status);
        if (status.done && pollRef.current) {
          clearInterval(pollRef.current);
          pollRef.current = null;
        }
      }, 1000);
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка запуска рассылки", "error");
    }
  };

  return (
    <div class="stack">
      <GlassCard>
        <div class="stack">
          <div class="field">
            <label class="field-label">Текст сообщения (HTML)</label>
            <textarea
              class="input"
              rows={6}
              value={text}
              onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
              placeholder="Текст рассылки…"
            />
          </div>
          <div class="field" style={{ maxWidth: 280 }}>
            <label class="field-label">Аудитория</label>
            <select class="select" value={segment} onChange={(e) => setSegment((e.target as HTMLSelectElement).value)}>
              {SEGMENTS.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
            </select>
          </div>
          <div class="row">
            <button class="btn btn-primary" onClick={openConfirm}>Отправить рассылку</button>
          </div>
        </div>
      </GlassCard>

      {job && (
        <GlassCard>
          <div class="stat-tile-label" style={{ marginBottom: 10 }}>
            Прогресс {job.done ? "(завершено)" : "…"}
          </div>
          <div class="row" style={{ gap: 24 }}>
            <span>Отправлено: <span class="mono">{job.sent}</span> / {job.total}</span>
            <span>Ошибок: <span class="mono">{job.failed}</span></span>
            <span>Недоступны: <span class="mono">{job.unreachable}</span></span>
          </div>
        </GlassCard>
      )}

      <ConfirmModal
        open={confirmOpen}
        title="Отправить рассылку?"
        body={`Получателей: ${previewCount ?? "…"}. Это действие нельзя отменить после запуска.`}
        confirmLabel="Отправить"
        danger
        onCancel={() => setConfirmOpen(false)}
        onConfirm={send}
      />
    </div>
  );
}
