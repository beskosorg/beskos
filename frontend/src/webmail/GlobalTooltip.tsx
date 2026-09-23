import { useEffect } from "react";

// GLOBAL, instant, styled tooltip. Instead of editing every button, it intercepts ANY element
// with `title` (or `data-tip`) on hover/focus, shows a nice bubble, and suppresses the OS's
// native tooltip (slow and inconsistent). Project rule: every icon-only button has a `title` —
// this manager turns all of them into one coherent tooltip. `data-tip-side` (right/left/top/
// bottom) forces the side; by default it sits below and flips up if it doesn't fit.

const DELAY = 180; // ms — short, but avoids "flickering" while sweeping the mouse across the screen

export function GlobalTooltip() {
  useEffect(() => {
    // No hover (touch): doesn't make sense; avoids bubbles stuck on phones/tablets.
    if (matchMedia("(hover: none)").matches) return;

    const bubble = document.createElement("div");
    bubble.className = "gtt";
    bubble.setAttribute("role", "tooltip");
    document.body.appendChild(bubble);

    let current: HTMLElement | null = null;
    let timer: number | undefined;

    const textOf = (el: HTMLElement) =>
      (el.getAttribute("data-tip") || el.getAttribute("data-tiptext") || el.getAttribute("title") || "").trim();

    // Walks up the tree to an element with a hint (the mouse target can be the <svg> inside the button).
    const findTarget = (node: EventTarget | null): HTMLElement | null => {
      let el = node as HTMLElement | null;
      for (let i = 0; el && i < 6; i++) {
        if (el.nodeType === 1 && (el.hasAttribute?.("data-tip") || el.hasAttribute?.("title")) && textOf(el)) return el;
        el = el.parentElement;
      }
      return null;
    };

    const place = (el: HTMLElement) => {
      const side = el.getAttribute("data-tip-side") || "bottom";
      const r = el.getBoundingClientRect();
      const gap = 8;
      // Measures first (the bubble already has the text and is visible but off-screen).
      const bw = bubble.offsetWidth, bh = bubble.offsetHeight;
      let left: number, top: number, resolved = side;
      const centerX = r.left + r.width / 2 - bw / 2;
      const centerY = r.top + r.height / 2 - bh / 2;
      if (side === "right") { left = r.right + gap; top = centerY; }
      else if (side === "left") { left = r.left - gap - bw; top = centerY; }
      else if (side === "top") { left = centerX; top = r.top - gap - bh; }
      else { left = centerX; top = r.bottom + gap; } // bottom
      // Flips bottom↔top if it doesn't fit vertically.
      if (resolved === "bottom" && top + bh > innerHeight - 4) { top = r.top - gap - bh; resolved = "top"; }
      else if (resolved === "top" && top < 4) { top = r.bottom + gap; resolved = "bottom"; }
      // Never leaves the screen.
      left = Math.max(6, Math.min(left, innerWidth - bw - 6));
      top = Math.max(6, Math.min(top, innerHeight - bh - 6));
      bubble.style.left = `${Math.round(left)}px`;
      bubble.style.top = `${Math.round(top)}px`;
    };

    const show = (el: HTMLElement) => {
      const text = textOf(el);
      if (!text) return;
      // Suppresses the native one: stores the title in data-tiptext and removes it while the bubble is visible.
      if (el.hasAttribute("title")) {
        el.setAttribute("data-tiptext", el.getAttribute("title") || "");
        el.removeAttribute("title");
      }
      bubble.textContent = text;
      bubble.classList.add("on");
      place(el);
    };

    const hide = () => {
      if (timer) { clearTimeout(timer); timer = undefined; }
      bubble.classList.remove("on");
      if (current?.hasAttribute("data-tiptext")) {
        current.setAttribute("title", current.getAttribute("data-tiptext") || "");
        current.removeAttribute("data-tiptext");
      }
      current = null;
    };

    const enter = (node: EventTarget | null) => {
      const el = findTarget(node);
      if (el === current) return;
      hide();
      if (!el) return;
      current = el;
      timer = window.setTimeout(() => { if (current === el) show(el); }, DELAY);
    };

    const onOver = (e: MouseEvent) => enter(e.target);
    const onOut = (e: MouseEvent) => {
      if (current && !current.contains(e.relatedTarget as Node)) hide();
    };
    const onFocus = (e: FocusEvent) => enter(e.target);
    const onBlur = () => hide();

    document.addEventListener("mouseover", onOver);
    document.addEventListener("mouseout", onOut);
    document.addEventListener("focusin", onFocus);
    document.addEventListener("focusout", onBlur);
    // Disappears on click/scroll/keypress — otherwise it stays hanging around.
    document.addEventListener("click", hide, true);
    window.addEventListener("scroll", hide, true);
    window.addEventListener("keydown", hide, true);

    return () => {
      hide();
      document.removeEventListener("mouseover", onOver);
      document.removeEventListener("mouseout", onOut);
      document.removeEventListener("focusin", onFocus);
      document.removeEventListener("focusout", onBlur);
      document.removeEventListener("click", hide, true);
      window.removeEventListener("scroll", hide, true);
      window.removeEventListener("keydown", hide, true);
      bubble.remove();
    };
  }, []);
  return null;
}
