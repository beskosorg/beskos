import { useEffect, useSyncExternalStore, type ReactNode } from "react";
import { IcX } from "./icons";

// Panel primitives: Modal, Drawer (sidecar) and Toast. No alert()/prompt().

export function Modal({ title, onClose, children, footer }: {
  title: string; onClose: () => void; children: ReactNode; footer?: ReactNode;
}) {
  useEscClose(onClose);
  return (
    <div className="pnl-modal-bg" onMouseDown={onClose}>
      <div className="pnl-modal" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-modal>
        <div className="pnl-modal-h">{title}<span className="grow" /><button className="pnl-icon-btn" onClick={onClose} aria-label="Fechar"><IcX size={18} /></button></div>
        <div className="pnl-modal-b">{children}</div>
        {footer && <div className="pnl-modal-f">{footer}</div>}
      </div>
    </div>
  );
}

export function Drawer({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  useEscClose(onClose);
  return (
    <div className="pnl-drawer-bg" onMouseDown={onClose}>
      <div className="pnl-drawer" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-modal>
        <div className="pnl-drawer-h">{title}<span className="grow" /><button className="pnl-icon-btn" onClick={onClose} aria-label="Fechar"><IcX size={18} /></button></div>
        <div className="pnl-drawer-b">{children}</div>
      </div>
    </div>
  );
}

function useEscClose(onClose: () => void) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [onClose]);
}

// ---- Toast global ----
let msg = "";
let timer: ReturnType<typeof setTimeout> | null = null;
const subs = new Set<() => void>();
export function toast(m: string) {
  msg = m; subs.forEach((s) => s());
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => { msg = ""; subs.forEach((s) => s()); }, 2800);
}
export function ToastHost() {
  const m = useSyncExternalStore((cb) => { subs.add(cb); return () => subs.delete(cb); }, () => msg);
  return <div className={"pnl-toast" + (m ? " show" : "")}>{m}</div>;
}
