import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";

import { colleaguesApi, contactsApi } from "./backend";
import { hueFor } from "./util";
import { Avatar } from "./Avatar";
import { IcSearch } from "../panel/icons";

// Chosen invitee: colleague (with userId), contact, or a typed external email.
export interface Invitee {
  email: string;
  name?: string;
  userId?: string; // present = internal colleague
  status?: string; // existing rsvp (accepted/declined/maybe), when applicable
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

// InviteePicker: ONE pattern for picking invitees across the app (events, Meet): searches
// COLLEAGUES + CONTACTS as you type, and accepts a typed EXTERNAL EMAIL. Chips show the
// avatar/initial in the person's color (hueFor) + ✕ — the same look used for invites.
export function InviteePicker({ value, onChange, autoFocus }: {
  value: Invitee[];
  onChange: (v: Invitee[]) => void;
  autoFocus?: boolean;
}) {
  const { t } = useTranslation();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false });
  const contacts = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });

  const picked = new Set(value.map((v) => v.email.toLowerCase()));
  const term = q.trim().toLowerCase();

  const suggestions = useMemo(() => {
    if (!term) return [];
    const out: (Invitee & { kind: "colleague" | "contact" })[] = [];
    for (const c of colleagues.data ?? []) {
      if (picked.has(c.email.toLowerCase())) continue;
      if (c.email.toLowerCase().includes(term) || (c.name ?? "").toLowerCase().includes(term)) {
        out.push({ email: c.email, name: c.name, userId: c.id, kind: "colleague" });
      }
    }
    for (const c of contacts.data ?? []) {
      const email = (c.email ?? "").toLowerCase();
      if (!email || picked.has(email) || out.some((o) => o.email.toLowerCase() === email)) continue;
      if (email.includes(term) || (c.name ?? "").toLowerCase().includes(term)) {
        out.push({ email: c.email!, name: c.name, kind: "contact" });
      }
    }
    return out.slice(0, 6);
  }, [term, colleagues.data, contacts.data, value]);

  const isEmail = EMAIL_RE.test(q.trim());
  const emailIsNew = isEmail && !picked.has(q.trim().toLowerCase()) &&
    !suggestions.some((s) => s.email.toLowerCase() === q.trim().toLowerCase());

  const add = (inv: Invitee) => {
    onChange([...value, inv]);
    setQ("");
    inputRef.current?.focus();
  };
  const remove = (email: string) => onChange(value.filter((v) => v.email !== email));

  return (
    <div className="invp">
      {value.length > 0 && (
        <div className="invp-chips">
          {value.map((v) => (
            <span key={v.email} className="invp-chip" title={v.name ? `${v.name} (${v.email})` : v.email}>
              <Avatar name={v.name} email={v.email} userId={v.userId} size={22} />
              <span className="nm">{v.name || v.email}</span>
              {!v.userId && <em className="ext">{t("invp.external")}</em>}
              {v.status && v.status !== "pending" && <span className={"rsvp-tag " + v.status}>{t("cal.rsvp_" + v.status)}</span>}
              <button type="button" title={t("common.remove")} onClick={() => remove(v.email)}>✕</button>
            </span>
          ))}
        </div>
      )}
      <div className="invp-search">
        <IcSearch size={14} />
        <input ref={inputRef} value={q} autoFocus={autoFocus}
          placeholder={t("invp.placeholder")}
          onChange={(e) => { setQ(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              if (suggestions.length > 0) add({ email: suggestions[0].email, name: suggestions[0].name, userId: suggestions[0].userId });
              else if (emailIsNew) add({ email: q.trim() });
            }
          }} />
      </div>
      {open && (suggestions.length > 0 || emailIsNew) && (
        <div className="invp-sugg">
          {suggestions.map((s) => (
            <button key={s.email} type="button"
              onMouseDown={(e) => { e.preventDefault(); add({ email: s.email, name: s.name, userId: s.userId }); }}>
              <Avatar name={s.name} email={s.email} userId={s.userId} size={26} />
              <span className="cinfo"><b>{s.name || s.email}</b><i>{s.email}</i></span>
              <span className="src">{t(s.kind === "colleague" ? "invp.colleague" : "invp.contact")}</span>
            </button>
          ))}
          {emailIsNew && (
            <button type="button" className="invp-ext"
              onMouseDown={(e) => { e.preventDefault(); add({ email: q.trim() }); }}>
              <span className="av" style={{ background: hueFor(q.trim()) }}>@</span>
              <span className="cinfo"><b>{t("invp.inviteExternal")}</b><i>{q.trim()}</i></span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}
