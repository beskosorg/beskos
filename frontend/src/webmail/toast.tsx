import { useSyncExternalStore } from "react";

// Simple global toast (action feedback). No dependencies.
// Supports an optional action (e.g. "Undo") with its own lifetime — used by
// "undo send", where the cancellation window is longer than a regular alert's.
interface ToastState {
  message: string;
  action?: { label: string; onClick: () => void };
}
let state: ToastState = { message: "" };
let timer: ReturnType<typeof setTimeout> | null = null;
const subs = new Set<() => void>();
const emit = () => subs.forEach((s) => s());

function show(next: ToastState, ms: number) {
  state = next;
  emit();
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => { state = { message: "" }; emit(); }, ms);
}

export function toast(message: string) {
  show({ message }, 2600);
}

// Toast with an action button. Stays visible for `ms` (default 5s) to give time to react.
export function toastAction(message: string, label: string, onClick: () => void, ms = 5000) {
  show({ message, action: { label, onClick } }, ms);
}

// Closes the toast immediately (e.g. after the action is used).
export function dismissToast() {
  if (timer) clearTimeout(timer);
  state = { message: "" };
  emit();
}

export function ToastHost() {
  const s = useSyncExternalStore((cb) => { subs.add(cb); return () => subs.delete(cb); }, () => state);
  return (
    <div className={"toast" + (s.message ? " show" : "")}>
      <span>{s.message}</span>
      {s.action && (
        <button className="toast-action" onClick={() => { s.action?.onClick(); dismissToast(); }}>
          {s.action.label}
        </button>
      )}
    </div>
  );
}
