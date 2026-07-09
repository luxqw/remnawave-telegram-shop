import { useState } from "preact/hooks";
import type { ComponentChildren } from "preact";

// ConfirmModal never trusts data handed to it from a table row — the caller's onOpen (if
// provided) is expected to re-fetch live state before rendering the confirmation body, mirroring
// the backend rule that every adminops mutation re-validates against fresh Remnawave state right
// before writing.
export function ConfirmModal(props: {
  open: boolean;
  title: string;
  body: ComponentChildren;
  confirmLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void | Promise<void>;
  onCancel: () => void;
}) {
  const [pending, setPending] = useState(false);

  if (!props.open) return null;

  const handleConfirm = async () => {
    setPending(true);
    try {
      await props.onConfirm();
    } finally {
      setPending(false);
    }
  };

  const busy = props.busy || pending;

  return (
    <div class="modal-backdrop" onClick={props.onCancel}>
      <div class="modal" onClick={(e) => e.stopPropagation()}>
        <div class="modal-title">{props.title}</div>
        <div class="modal-body">{props.body}</div>
        <div class="modal-actions">
          <button class="btn" onClick={props.onCancel} disabled={busy}>
            Отменить
          </button>
          <button
            class={props.danger ? "btn btn-danger" : "btn btn-primary"}
            onClick={handleConfirm}
            disabled={busy}
          >
            {busy ? "Выполняю…" : (props.confirmLabel ?? "Подтвердить")}
          </button>
        </div>
      </div>
    </div>
  );
}
