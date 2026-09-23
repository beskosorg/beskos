import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { contactsApi, type Contact, type ContactInput, type ContactKind } from "./backend";
import { Drawer } from "./Drawer";
import { toast } from "./toast";
import { IcTrash, IcX, IcPlus } from "../panel/icons";

// New contact: consent is enabled by default (whoever creates it normally has permission), but
// without a group the contact is only visible to its owner — opening it up is a deliberate decision, made at the group level.
const empty: ContactInput = {
  kind: "person", name: "", email: "", phone: "", company: "", domain: "", notes: "",
  tags: [], group_ids: [], title: "", mobile: "", is_whatsapp: false,
  allow_phone: true, allow_email: true, company_id: null, extra_emails: [], extra_phones: [],
};

export function ContactDrawer({ contact, initial, onClose, onOpen, onSaved, onDeleted }: {
  contact: Contact | null;
  initial?: Partial<ContactInput>; // pre-filled creation (e.g. a new person inside a company)
  onClose: () => void;
  onOpen?: (id: string) => void; onSaved?: () => void; onDeleted?: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [f, setF] = useState<ContactInput>(contact ? { ...empty, ...contact } : { ...empty, ...initial });
  const [tagInput, setTagInput] = useState("");
  const set = (k: keyof ContactInput, v: unknown) => setF((p) => ({ ...p, [k]: v }));
  // Custom fields: edited as {k,v} rows; synced into the custom_fields object.
  const [cfRows, setCfRows] = useState<{ k: string; v: string }[]>(Object.entries(contact?.custom_fields ?? {}).map(([k, v]) => ({ k, v })));
  const syncCf = (rows: { k: string; v: string }[]) => {
    setCfRows(rows);
    set("custom_fields", Object.fromEntries(rows.filter((r) => r.k.trim()).map((r) => [r.k.trim(), r.v])));
  };

  const all = useQuery({ queryKey: ["contacts"], queryFn: () => contactsApi.list(), retry: false });
  const groups = useQuery({ queryKey: ["contact-groups"], queryFn: () => contactsApi.groups(), retry: false });
  const companies = (all.data ?? []).filter((c) => c.kind === "company" && c.id !== contact?.id);

  const done = () => {
    qc.invalidateQueries({ queryKey: ["contacts"] });
    if (contact) qc.invalidateQueries({ queryKey: ["contact", contact.id] });
  };
  const save = useMutation({
    mutationFn: () => (contact ? contactsApi.update(contact.id, f) : contactsApi.create(f)),
    onSuccess: (c) => { toast(t("contacts.saved")); done(); onClose(); onSaved?.(); if (!contact) onOpen?.(c.id); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({
    mutationFn: () => contactsApi.remove(contact!.id),
    onSuccess: () => { toast(t("contacts.deleted")); done(); onClose(); onDeleted?.(); },
    onError: (e) => toast((e as Error).message),
  });

  const addTag = () => {
    const v = tagInput.trim();
    if (v && !f.tags.includes(v)) set("tags", [...f.tags, v]);
    setTagInput("");
  };
  const isCompany = f.kind === "company";

  return (
    <Drawer title={contact ? t("contacts.edit") : t("contacts.add")} onClose={onClose}
      footer={<>
        {contact && <button className="mc-cancel" onClick={() => del.mutate()}><IcTrash size={15} /> {t("contacts.delete")}</button>}
        <div className="grow" />
        <button className="add" disabled={!f.name.trim() || save.isPending} onClick={() => save.mutate()}>{t("contacts.save")}</button>
      </>}>
          <div className="field"><label>{t("contacts.kind")}</label>
            <div className="crm-filters">
              {(["company", "person"] as const).map((k) => (
                <button key={k} type="button" className={"fchip" + (f.kind === k ? " on" : "")}
                  onClick={() => set("kind", k as ContactKind)}>{t(`contacts.kind_${k}`)}</button>
              ))}
            </div>
          </div>

          <div className="field"><label>{t("contacts.name")}</label><input autoFocus value={f.name} onChange={(e) => set("name", e.target.value)} /></div>

          {isCompany
            ? <div className="field"><label>{t("crm.domain")}</label><input value={f.domain} onChange={(e) => set("domain", e.target.value)} placeholder="empresa.com.br" /></div>
            : (
              <>
                <div className="field"><label>{t("crm.title")}</label><input value={f.title} onChange={(e) => set("title", e.target.value)} placeholder={t("crm.titlePh")} /></div>
                <div className="field"><label>{t("crm.company")}</label>
                  <select className="cts-select" value={f.company_id ?? ""} onChange={(e) => set("company_id", e.target.value || null)}>
                    <option value="">{t("contacts.noCompany")}</option>
                    {companies.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
                  </select>
                </div>
                {!f.company_id && (
                  <div className="field"><label>{t("contacts.companyFree")}</label><input value={f.company} onChange={(e) => set("company", e.target.value)} /></div>
                )}
              </>
            )}

          <div className="field"><label>{t("contacts.email")}</label><input type="email" value={f.email} onChange={(e) => set("email", e.target.value)} /></div>
          <MultiInput label={t("contacts.extraEmails")} values={f.extra_emails} type="email"
            placeholder={t("contacts.addEmail")} onChange={(v) => set("extra_emails", v)} />
          <div className="field"><label>{t("contacts.phone")}</label><input value={f.phone} onChange={(e) => set("phone", e.target.value)} /></div>
          {!isCompany && <div className="field"><label>{t("crm.mobile")}</label><input value={f.mobile} onChange={(e) => set("mobile", e.target.value)} /></div>}
          <MultiInput label={t("contacts.extraPhones")} values={f.extra_phones}
            placeholder={t("contacts.addPhone")} onChange={(v) => set("extra_phones", v)} />

          <div className="field"><label>{t("crm.consent")}</label>
            <div className="grp-picker">
              {!isCompany && (
                <label className={"grp-chk" + (f.is_whatsapp ? " on" : "")}>
                  <input type="checkbox" checked={f.is_whatsapp} onChange={(e) => set("is_whatsapp", e.target.checked)} />
                  <span>{t("crm.isWhatsapp")}</span>
                </label>
              )}
              <label className={"grp-chk" + (f.allow_phone ? " on" : "")}>
                <input type="checkbox" checked={f.allow_phone} onChange={(e) => set("allow_phone", e.target.checked)} />
                <span>{t("crm.allowPhone")}</span>
              </label>
              <label className={"grp-chk" + (f.allow_email ? " on" : "")}>
                <input type="checkbox" checked={f.allow_email} onChange={(e) => set("allow_email", e.target.checked)} />
                <span>{t("crm.allowEmail")}</span>
              </label>
            </div>
          </div>

          <div className="field"><label>{t("contacts.groups")}</label>
            {(groups.data ?? []).length === 0 ? <p className="desc">{t("contacts.noGroups")}</p> : (
              <div className="grp-picker">
                {(groups.data ?? []).map((g) => {
                  const on = f.group_ids.includes(g.id);
                  return (
                    <label key={g.id} className={"grp-chk" + (on ? " on" : "")}>
                      <input type="checkbox" checked={on}
                        onChange={() => set("group_ids", on ? f.group_ids.filter((x) => x !== g.id) : [...f.group_ids, g.id])} />
                      <span className="dot" style={{ background: g.color }} />
                      <span>{g.name}</span>
                    </label>
                  );
                })}
              </div>
            )}
            <p className="desc">{t("contacts.groupsHint")}</p>
          </div>

          <div className="field"><label>{t("contacts.tags")}</label>
            <div className="tp-chips">{f.tags.map((tg) => (
              <span className="tp-chip" key={tg} onClick={() => set("tags", f.tags.filter((x) => x !== tg))}>{tg} ×</span>
            ))}</div>
            <input value={tagInput} placeholder={t("contacts.tagsPh")} onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addTag(); } }} onBlur={addTag} />
          </div>

          <div className="field"><label>{t("crm.customFields")}</label>
            {cfRows.map((row, i) => (
              <div className="multi-row" key={i}>
                <input value={row.k} placeholder={t("crm.fieldName")} style={{ maxWidth: "42%" }}
                  onChange={(e) => syncCf(cfRows.map((r, j) => (j === i ? { ...r, k: e.target.value } : r)))} />
                <input value={row.v} placeholder={t("crm.fieldValue")}
                  onChange={(e) => syncCf(cfRows.map((r, j) => (j === i ? { ...r, v: e.target.value } : r)))} />
                <button type="button" className="ib" title={t("contacts.remove")} onClick={() => syncCf(cfRows.filter((_, j) => j !== i))}><IcX size={14} /></button>
              </div>
            ))}
            <button type="button" className="multi-add" onClick={() => syncCf([...cfRows, { k: "", v: "" }])}><IcPlus size={13} /> {t("crm.addField")}</button>
          </div>

          <div className="field"><label>{t("contacts.notes")}</label><textarea rows={3} value={f.notes} onChange={(e) => set("notes", e.target.value)} /></div>
    </Drawer>
  );
}

// Editor for a list of values (secondary emails/phones): one row per value + an add button.
// Keeps empty entries in state while typing; the backend clears empty ones on save.
function MultiInput({ label, values, onChange, placeholder, type }: {
  label: string; values: string[]; onChange: (v: string[]) => void; placeholder: string; type?: string;
}) {
  const { t } = useTranslation();
  const setAt = (i: number, v: string) => onChange(values.map((x, j) => (j === i ? v : x)));
  const removeAt = (i: number) => onChange(values.filter((_, j) => j !== i));
  return (
    <div className="field">
      <label>{label}</label>
      {values.map((v, i) => (
        <div className="multi-row" key={i}>
          <input type={type} value={v} placeholder={placeholder} onChange={(e) => setAt(i, e.target.value)} />
          <button type="button" className="ib" title={t("contacts.remove")} onClick={() => removeAt(i)}><IcX size={14} /></button>
        </div>
      ))}
      <button type="button" className="multi-add" onClick={() => onChange([...values, ""])}><IcPlus size={13} /> {placeholder}</button>
    </div>
  );
}
