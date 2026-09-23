// Suspending, deleting and recovering a mailbox, from the people list.
//
// The wording here is doing real work. An admin about to delete somebody's mailbox is usually in a
// hurry and rarely reads twice, so the screen has to say the two things that are actually true and
// that nobody expects: nothing is destroyed today, and mail to that address stops arriving unless
// they name somebody to take it. Both are decisions, and neither has a safe default we could pick
// for them.

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Mailbox } from "../api";
import { Modal, toast } from "./ui";
import { IcTrash, IcKey, IcDownload, IcRefresh } from "./icons";

export function MailboxStatusPill({ mb }: { mb: Mailbox }) {
  const { t, i18n } = useTranslation();
  if (mb.status === "active") return null; // the normal case needs no decoration
  const tone = mb.status === "deleted" ? "danger" : "warn";
  const when = mb.purge_after
    ? new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium" }).format(new Date(mb.purge_after))
    : "";
  return (
    <span className={`pnl-pill ${tone}`} title={when ? t("mailboxes.purgeOn", { date: when }) : undefined}>
      {t(mb.status === "deleted" ? "mailboxes.statusDeleted" : "mailboxes.statusSuspended")}
    </span>
  );
}

export function MailboxActions({ mb }: { mb: Mailbox }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [deleting, setDeleting] = useState(false);
  const [suspending, setSuspending] = useState(false);
  const done = () => qc.invalidateQueries({ queryKey: ["admin-mailboxes"] });

  const setStatus = useMutation({
    mutationFn: (status: "active" | "suspended") => api.setMailboxStatus(mb.id, status),
    onSuccess: (_d, status) => { done(); toast(t(status === "suspended" ? "mailboxes.suspended" : "mailboxes.reactivated")); },
    onError: (e) => toast((e as Error).message),
  });
  const restore = useMutation({
    mutationFn: () => api.restoreMailbox(mb.id),
    onSuccess: () => { done(); toast(t("mailboxes.restored")); },
    onError: (e) => toast((e as Error).message),
  });

  if (mb.status === "deleted") {
    return (
      <>
        {/* Export before restore in the row order: during these thirty days it is the thing that
            cannot be done later, and the one somebody came here for after a mistake. */}
        <a className="pnl-icon-btn" title={t("mailboxes.export")} href={api.mailboxExportURL(mb.id)} download>
          <IcDownload size={16} />
        </a>
        <button className="pnl-icon-btn" title={t("mailboxes.restore")} onClick={() => restore.mutate()}>
          <IcRefresh size={16} />
        </button>
      </>
    );
  }

  return (
    <>
      {mb.status === "active"
        ? <button className="pnl-icon-btn" title={t("mailboxes.suspend")} onClick={() => setSuspending(true)}>
            <IcKey size={16} />
          </button>
        : <button className="pnl-icon-btn" title={t("mailboxes.reactivate")} onClick={() => setStatus.mutate("active")}>
            <IcRefresh size={16} />
          </button>}
      <button className="pnl-icon-btn danger" title={t("mailboxes.delete")} onClick={() => setDeleting(true)}>
        <IcTrash size={16} />
      </button>
      {deleting && <DeleteMailboxModal mb={mb} onClose={() => setDeleting(false)} onDone={() => { setDeleting(false); done(); }} />}
      {/* Suspension is reversible and loses nothing, so it gets one click of friction rather than a
          form -- but it does lock a colleague out of their work, so it does not get silence either. */}
      {suspending && (
        <Modal title={t("mailboxes.suspend")} onClose={() => setSuspending(false)}
          footer={<>
            <button className="pnl-btn ghost" onClick={() => setSuspending(false)}>{t("common.cancel")}</button>
            <button className="pnl-btn primary" onClick={() => { setSuspending(false); setStatus.mutate("suspended"); }}>{t("mailboxes.suspend")}</button>
          </>}>
          <p className="desc">{t("mailboxes.suspendConfirm", { email: mb.email })}</p>
          <p className="pnl-note">{t("mailboxes.reactivateHint")}</p>
        </Modal>
      )}
    </>
  );
}

function DeleteMailboxModal({ mb, onClose, onDone }: { mb: Mailbox; onClose: () => void; onDone: () => void }) {
  const { t } = useTranslation();
  const [successor, setSuccessor] = useState("");
  const [understood, setUnderstood] = useState(false);
  const [err, setErr] = useState("");

  const run = useMutation({
    mutationFn: () => api.deleteMailbox(mb.id, successor.trim()),
    onSuccess: () => { onDone(); toast(t("mailboxes.deleted")); },
    onError: (e) => setErr((e as Error).message),
  });

  return (
    <Modal title={t("mailboxes.deleteTitle", { email: mb.email })} onClose={onClose}
      footer={
        <>
          <button className="pnl-btn ghost" onClick={onClose}>{t("common.cancel")}</button>
          <button className="pnl-btn danger" disabled={!understood || run.isPending} onClick={() => run.mutate()}>
            {t("mailboxes.delete")}
          </button>
        </>
      }>
      <p className="desc">{t("mailboxes.deleteBlurb")}</p>

      <div className="pnl-field">
        <label>{t("mailboxes.deleteSuccessor")}</label>
        <input className="pnl-input" value={successor} autoComplete="off"
          placeholder={t("mailboxes.deleteSuccessorNone")}
          onChange={(e) => setSuccessor(e.target.value)} />
        <div className="pnl-note">{t("mailboxes.deleteSuccessorHint")}</div>
      </div>

      <label className="mbx-confirm">
        <input type="checkbox" checked={understood} onChange={(e) => setUnderstood(e.target.checked)} />
        <span>{t("mailboxes.deleteConfirm")}</span>
      </label>

      {err && <p className="pnl-err">{err}</p>}
    </Modal>
  );
}
