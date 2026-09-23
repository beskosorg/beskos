import { useEffect, useState } from "react";
import { createPortal } from "react-dom";

// Slot to place OUR buttons inside the LiveKit control bar, instead of floating
// above it.
//
// Why a portal and not simply positioning alongside it: the bar is rendered by
// <VideoConference/> and its width changes with the language, the devices, and the chat button.
// Any absolute positioning next to it is a guess that fails as soon as it grows — that's how
// the icons ended up overlapping. Being children of the same flex, they share the row and wrap
// together, with no magic number at all.
//
// Rebuilding the bar from scratch with LiveKit's hooks was the other option, but ChatToggle depends
// on <VideoConference/>'s layout context and chat would stop working.

/** Returns a node inside the LiveKit bar, or null while it doesn't exist yet. */
export function useControlBarSlot(): HTMLElement | null {
  const [slot, setSlot] = useState<HTMLElement | null>(null);

  useEffect(() => {
    let mine: HTMLDivElement | null = null;

    const attach = () => {
      if (mine?.isConnected) return; // already there
      const bar = document.querySelector<HTMLElement>(".lk-control-bar");
      if (!bar) return;
      mine = document.createElement("div");
      // display:contents — the wrapper disappears from the layout and the buttons become direct children of the row.
      mine.className = "room-actions";
      bar.appendChild(mine);
      setSlot(mine);
    };

    attach();
    // The bar only appears after the room connects, and LiveKit remounts it on reconnect.
    const obs = new MutationObserver(attach);
    obs.observe(document.body, { childList: true, subtree: true });
    return () => {
      obs.disconnect();
      mine?.remove();
    };
  }, []);

  return slot;
}

/**
 * Destination for full-screen overlays (presentation stage, raised hands, modals).
 *
 * `.wm` and NOT `document.body`: the whole design system is scoped to `.wm` — the variables
 * (`--surface`, `--ink`) and the utility classes (`.chipbtn`, `.ib`). Outside it `var(--surface)`
 * doesn't resolve and the buttons lose their styling. `.wm` has no transform/filter/contain, so
 * `position:fixed` still measures itself against the screen — which is why it leaves the control bar.
 */
export function overlayRoot(): HTMLElement {
  return document.querySelector<HTMLElement>(".wm") ?? document.body;
}

/** Renders the children inside the LiveKit bar. While it doesn't exist yet, renders nothing. */
export function ControlBarSlot({ children }: { children: React.ReactNode }) {
  const slot = useControlBarSlot();
  return slot ? createPortal(children, slot) : null;
}
