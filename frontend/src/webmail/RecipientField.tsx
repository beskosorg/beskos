import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { contactsApi, colleaguesApi, type Contact, type ContactGroup } from "./backend";
import { toast } from "./toast";
import { hueFor } from "./util";
import { Avatar } from "./Avatar";
import { IcUsers } from "../panel/icons";

// Recipient field as chips with contact autocomplete. Reused by To/Cc/Bcc.
// Contact groups appear as distribution lists: picking one expands to all the emails.
export function RecipientField({ label, value, onChange, contacts, groups, autoFocus }: {
  label: string;
  value: string[];
  onChange: (v: string[]) => void;
  contacts: Contact[];
  groups?: ContactGroup[];
  autoFocus?: boolean;
}) {
  const { t } = useTranslation();
  const [input, setInput] = useState("");
  const q = input.trim().toLowerCase();
  // Colleagues in the organization are ALWAYS suggestible (even if never emailed before); contacts
  // include people already reached (sending saves them via /contacts/seen). Colleagues first,
  // deduplicated by email.
  const colleaguesQ = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, staleTime: 300000 });
  const suggestions = q
    ? (() => {
        const seen = new Set(value.map((e) => e.toLowerCase()));
        const out: { id: string; name: string; email: string; team?: boolean }[] = [];
        for (const c of colleaguesQ.data ?? []) {
          if (!c.email || seen.has(c.email.toLowerCase())) continue;
          if (c.email.toLowerCase().includes(q) || (c.name ?? "").toLowerCase().includes(q)) {
            out.push({ id: c.id, name: c.name || c.email, email: c.email, team: true });
            seen.add(c.email.toLowerCase());
          }
        }
        for (const c of contacts) {
          if (!c.email || seen.has(c.email.toLowerCase())) continue;
          if (c.email.toLowerCase().includes(q) || c.name.toLowerCase().includes(q)) {
            out.push({ id: c.id, name: c.name || c.email, email: c.email });
            seen.add(c.email.toLowerCase());
          }
        }
        return out.slice(0, 8);
      })()
    : [];
  const groupSuggestions = q ? (groups ?? []).filter((g) => g.name.toLowerCase().includes(q)).slice(0, 4) : [];

  const add = (email: string) => { const e = email.trim().replace(/[,;]$/, ""); if (e && !value.includes(e)) onChange([...value, e]); setInput(""); };
  // Distribution list: fetches the group's emails and adds the ones not already present.
  const addGroup = async (g: ContactGroup) => {
    setInput("");
    try {
      const members = await contactsApi.groupEmails(g.id);
      const emails = members.map((m) => m.email).filter((e) => e && !value.includes(e));
      if (emails.length === 0) { toast(t("webmail.compose.groupEmpty")); return; }
      onChange([...value, ...emails.filter((e, i) => emails.indexOf(e) === i)]);
      toast(t("webmail.compose.groupAdded", { count: emails.length, name: g.name }));
    } catch (e) { toast((e as Error).message); }
  };
  const onKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if ((e.key === "Enter" || e.key === "," || e.key === ";") && input.trim()) { e.preventDefault(); add(input); }
    else if (e.key === "Backspace" && !input && value.length) onChange(value.slice(0, -1));
  };

  return (
    <div className="rcpt">
      <label>{label}</label>
      <div className="rcpt-box">
        {value.map((e) => (
          <span key={e} className="rcpt-chip">{e}<button type="button" title={t("common.remove")} onClick={() => onChange(value.filter((x) => x !== e))}>✕</button></span>
        ))}
        <div className="rcpt-in">
          <input value={input} autoFocus={autoFocus} onChange={(e) => setInput(e.target.value)} onKeyDown={onKey}
            onBlur={() => input.trim() && add(input)} placeholder={t("webmail.compose.recipientPh")} />
          {(suggestions.length > 0 || groupSuggestions.length > 0) && (
            <div className="rcpt-sugg">
              {groupSuggestions.map((g) => (
                <button key={g.id} type="button" className="rcpt-group" onMouseDown={(ev) => { ev.preventDefault(); void addGroup(g); }}>
                  <span className="av" style={{ background: g.color || hueFor(g.name) }}><IcUsers size={13} /></span>
                  <span className="nm">{g.name}</span><span className="em">{t("webmail.compose.distributionList")}</span>
                </button>
              ))}
              {suggestions.map((c) => (
                <button key={c.id} type="button" onMouseDown={(ev) => { ev.preventDefault(); add(c.email); }}>
                  <Avatar name={c.name} email={c.email} size={26} />
                  <span className="nm">{c.name || c.email}</span><span className="em">{c.email}</span>
                  {c.team && <span className="rcpt-team">{t("invp.colleague")}</span>}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
