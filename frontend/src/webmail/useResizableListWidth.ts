// Resizable width of the mail list column, extracted from MailView.tsx (law 1.4 -- 600 lines).
// Persisted per device (localStorage) -- it depends on the screen, not the account.
import { useState } from "react";

const LIST_W_KEY = "mailin.wm.listWidth";
const LIST_W_MIN = 340, LIST_W_MAX = 680;

export function useResizableListWidth() {
  const [listW, setListW] = useState<number>(() => {
    const v = Number(localStorage.getItem(LIST_W_KEY));
    return v >= LIST_W_MIN && v <= LIST_W_MAX ? v : 420;
  });
  const [resizing, setResizing] = useState(false);
  const startResize = (e: React.PointerEvent) => {
    e.preventDefault();
    const startX = e.clientX, startW = listW;
    setResizing(true);
    const move = (ev: PointerEvent) => {
      setListW(Math.min(LIST_W_MAX, Math.max(LIST_W_MIN, startW + (ev.clientX - startX))));
    };
    const up = (ev: PointerEvent) => {
      const w = Math.min(LIST_W_MAX, Math.max(LIST_W_MIN, startW + (ev.clientX - startX)));
      localStorage.setItem(LIST_W_KEY, String(w));
      setResizing(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return { listW, resizing, startResize };
}
