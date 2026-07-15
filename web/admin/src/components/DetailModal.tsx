import type { ComponentChildren } from "preact";

// A read-only counterpart to ConfirmModal: shows a full record's fields with a single "Close"
// action, no confirm/cancel branching. Reuses the same .modal-backdrop/.modal CSS recipe.
export function DetailModal(props: { open: boolean; title: string; body: ComponentChildren; onClose: () => void }) {
  if (!props.open) return null;

  return (
    <div class="modal-backdrop" onClick={props.onClose}>
      <div class="modal" onClick={(e) => e.stopPropagation()}>
        <div class="modal-title">{props.title}</div>
        <div class="modal-body">{props.body}</div>
        <div class="modal-actions">
          <button class="btn" onClick={props.onClose}>
            Закрыть
          </button>
        </div>
      </div>
    </div>
  );
}
