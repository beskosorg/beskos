import { useSyncExternalStore } from "react";

// Global count of open SIDECARS/overlays: the assistant bubble hides itself while any are open
// (it used to overlap drawer footers). The Drawer registers itself.
let open = 0;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

export function overlayOpened(): () => void {
  open++;
  emit();
  let done = false;
  return () => { if (!done) { done = true; open--; emit(); } };
}

export function useOverlayOpen(): boolean {
  return useSyncExternalStore(
    (l) => { listeners.add(l); return () => listeners.delete(l); },
    () => open > 0,
  );
}
