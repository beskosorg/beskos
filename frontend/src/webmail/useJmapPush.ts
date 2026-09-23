import { useEffect, useRef } from "react";
import type { MailService } from "../jmap/mail";

// Keeps a JMAP push connection (SSE) alive while the component is mounted, reconnecting
// with backoff if it drops. Calls onState() on every account state change (new e-mail, read…).
// TanStack Query's polling remains as a safety net in case the proxy does not hold the SSE.
export function useJmapPush(service: MailService, onState: () => void): void {
  const cb = useRef(onState);
  cb.current = onState;

  useEffect(() => {
    const ctrl = new AbortController();
    let stopped = false;
    let delay = 2000;

    (async () => {
      while (!stopped) {
        try {
          await service.streamPush(() => cb.current(), ctrl.signal);
          delay = 2000; // clean close → reconnect fast
        } catch {
          if (ctrl.signal.aborted) return;
          delay = Math.min(delay * 2, 30000); // error → backoff up to 30s
        }
        if (stopped) return;
        await new Promise((r) => setTimeout(r, delay));
      }
    })();

    return () => { stopped = true; ctrl.abort(); };
  }, [service]);
}
