import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { colleaguesApi } from "./backend";
import { hueFor, initials } from "./util";

// Shared avatar: shows the user's PHOTO wherever their name appears (top bar, chat, kanban,
// meet, mail…). The photo either comes explicitly via the `src` prop, or is resolved by
// email/id from the colleagues DIRECTORY (which already carries the photo) — so any screen
// showing a colleague gets the photo without changing every payload. No photo falls back to
// the initials circle (hueFor).

// useAvatarDir: email→photo and id→photo maps, built from the tenant's colleagues (cached query).
function useAvatarDir() {
  const q = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, staleTime: 300000 });
  return useMemo(() => {
    const byEmail = new Map<string, string>();
    const byId = new Map<string, string>();
    for (const c of q.data ?? []) {
      if (c.avatar) { byEmail.set(c.email.toLowerCase(), c.avatar); byId.set(c.id, c.avatar); }
    }
    return { byEmail, byId };
  }, [q.data]);
}

export function Avatar({ name, email, userId, src, size = 28, radius = "50%", title, className, onClick }: {
  name?: string; email?: string; userId?: string; src?: string; size?: number; radius?: string; title?: string; className?: string; onClick?: () => void;
}) {
  const dir = useAvatarDir();
  const photo = src || (email ? dir.byEmail.get(email.toLowerCase()) : undefined) || (userId ? dir.byId.get(userId) : undefined);
  const key = email || userId || name || "?";
  const tip = title ?? (name && email ? `${name} (${email})` : name || email);
  const cls = "av" + (className ? " " + className : "");
  const box = { width: size, height: size, borderRadius: radius, flex: "none" as const, cursor: onClick ? "pointer" : undefined };
  if (photo) {
    return <img className={cls} src={photo} alt="" title={tip} onClick={onClick} style={{ ...box, objectFit: "cover" }} />;
  }
  return (
    <span className={cls} title={tip} onClick={onClick}
      style={{ ...box, background: hueFor(key), color: "#fff", display: "inline-grid", placeItems: "center", fontSize: Math.round(size * 0.4), fontWeight: 650 }}>
      {initials(name || email || "?")}
    </span>
  );
}
