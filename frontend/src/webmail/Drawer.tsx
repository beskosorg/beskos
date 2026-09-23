import { useEffect, type ReactNode } from "react";
import { IcX } from "../panel/icons";
import { overlayOpened } from "./overlay";

// Drawer is the STANDARD container for viewing/editing an item (event, task, contact): a panel
// docked to the right, with the context (calendar/list) still visible alongside it. One single
// place for the shared behavior — dimmed background, click-outside-to-close, ESC, body scroll,
// fixed footer, optional width for content with a side panel — so the app doesn't end up with
// two different patterns.
export function Drawer({ title, actions, footer, wide, onClose, children }: {
  title: ReactNode;
  // actions are header buttons to the right of the title (e.g. the "edit" pencil). The ✕ always
  // belongs to the Drawer, so the close gesture is the same everywhere.
  actions?: ReactNode;
  footer?: ReactNode;
  // wide widens the panel for content with a side column (e.g. a task with activity alongside it).
  wide?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  // ESC closes — the same shortcut across the whole app. Only the Drawer handles it: no screen
  // needs to reinvent it.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Sidecar open → the assistant bubble hides itself (registers/unregisters through the lifecycle).
  useEffect(() => overlayOpened(), []);

  return (
    <div className="drawer-bg open" onMouseDown={onClose}>
      <div className={"drawer" + (wide ? " drawer-wide" : "")} onMouseDown={(e) => e.stopPropagation()}>
        <header>
          <span className="dw-title">{title}</span>
          <div className="grow" />
          {actions}
          <button className="ib" onClick={onClose} aria-label="Close"><IcX size={15} /></button>
        </header>
        <div className="body">{children}</div>
        {footer && <footer>{footer}</footer>}
      </div>
    </div>
  );
}
