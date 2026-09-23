// Bringing a mailbox in from the provider the customer is leaving.
//
// THE SCREEN IS TWO STEPS AND THAT IS THE POINT. Step one connects and shows what was found,
// copying nothing. Step two starts the job. Somebody handing over the password to their old mail
// account wants to see that we reached it and what we are going to do, before we do it -- and
// somebody who mistyped the server name finds out in three seconds instead of an hour.
//
// The folders that will NOT be copied are listed as prominently as the ones that will. Being told
// afterwards that Trash and Gmail's All Mail were skipped is being told after an afternoon of
// believing we lost the mail.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, type Migration, type MigrationCheck, type MigrationFolder, type MigrationSource } from "../api";
import { PROVIDERS, OTHER_PROVIDER, providerById, guessProvider, type Provider } from "./migrationProviders";
import { MigrationFileForm } from "./MigrationFileForm";
import { toast } from "./ui";
import { IcDownload, IcCheck, IcX, IcRefresh } from "./icons";

// migrationQuery is shared with the first-access prompt, so both read one cached answer.
export const MIGRATION_QUERY_KEY = ["migration"];

export function MigrationSection() {
  const { t } = useTranslation();
  const current = useQuery({
    queryKey: MIGRATION_QUERY_KEY,
    queryFn: api.migration,
    // No retries, as everywhere else here. TanStack retries three times with exponential backoff by
    // default and keeps the query pending throughout, so a route that answers 404 -- an API not yet
    // deployed, say -- showed half a minute of "…" and no way to tell it was broken.
    retry: false,
    // While a job runs the numbers are the whole screen. Polling stops the moment it ends, so a
    // finished migration is not a page that talks to the server every three seconds forever.
    refetchInterval: (q) => {
      const s = q.state.data?.migration?.status;
      return s === "running" || s === "pending" ? 3000 : false;
    },
  });

  const job = current.data?.migration ?? null;
  const live = job && (job.status === "running" || job.status === "pending");

  // Same shell as every other panel screen: title band, then the content column. The heading is on
  // screen before the request answers -- a page that is only a spinner cannot even be identified.
  return (
    <>
      <div className="pnl-top">
        <div><h1>{t("migration.title")}</h1><div className="desc">{t("migration.blurb")}</div></div>
      </div>
      <div className="pnl-content">
        {current.isPending
          ? <div className="pnl-card"><div className="pnl-card-b"><div className="loading">…</div></div></div>
          : current.isError
            // The server could not be asked. Saying so beats showing a form whose first button would
            // fail the same way, with the same silence.
            ? <div className="pnl-card"><div className="pnl-card-b">
                <p className="pnl-err">{(current.error as Error).message}</p>
                <button className="pnl-btn" onClick={() => void current.refetch()}>{t("migration.retry")}</button>
              </div></div>
            : live ? <MigrationProgress job={job!} /> : <MigrationForm last={job} />}
      </div>
    </>
  );
}

// ---- Step one and two: the form ----

function MigrationForm({ last }: { last: Migration | null }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  // Which kind of source. "server" first because it is the common case; "file" is what somebody
  // reaches for once they discover their old mail is not on a server at all.
  const [source, setSource] = useState<"server" | "file">("server");
  const [provider, setProvider] = useState<Provider>(OTHER_PROVIDER);
  const [src, setSrc] = useState<MigrationSource>({ host: "", username: "", password: "", starttls: false });
  const [found, setFound] = useState<MigrationCheck | null>(null);
  const [err, setErr] = useState("");

  const check = useMutation({
    mutationFn: () => api.migrationCheck(src),
    onSuccess: (d) => { setFound(d); setErr(""); },
    onError: (e) => { setFound(null); setErr((e as Error).message); },
  });
  const start = useMutation({
    mutationFn: () => api.migrationStart(src),
    onSuccess: () => { qc.invalidateQueries({ queryKey: MIGRATION_QUERY_KEY }); toast(t("migration.started")); },
    onError: (e) => setErr((e as Error).message),
  });

  const ready = src.host.trim() && src.username.trim() && src.password;
  // Changing any field invalidates what we found: the list on screen would otherwise describe a
  // different account than the one about to be copied.
  const edit = (patch: Partial<MigrationSource>) => { setSrc((s) => ({ ...s, ...patch })); setFound(null); setErr(""); };

  const pick = (id: string) => {
    const p = providerById(id);
    setProvider(p);
    edit({ host: p.host, port: p.port, starttls: p.starttls });
  };

  // Typing the address is usually enough: a consumer domain names its own provider, and the person
  // who has one is the least likely to know what an IMAP server is called.
  const onAddress = (value: string) => {
    const guess = provider.id === "other" ? guessProvider(value) : null;
    if (guess) {
      setProvider(guess);
      edit({ username: value, host: guess.host, port: guess.port, starttls: guess.starttls });
      return;
    }
    edit({ username: value });
  };

  return (
    <>
      {last && <LastRun job={last} />}

      {/* Two ways in, named for what the customer has rather than for the protocol. */}
      <div className="mig-sources">
        <button type="button" className={"mig-src-tab" + (source === "server" ? " on" : "")}
          onClick={() => setSource("server")}>{t("migration.sourceServer")}</button>
        <button type="button" className={"mig-src-tab" + (source === "file" ? " on" : "")}
          onClick={() => setSource("file")}>{t("migration.sourceFile")}</button>
      </div>

      {source === "file" ? <MigrationFileForm /> : (
      <div className="pnl-card">
        <div className="pnl-card-h">
          <IcDownload size={16} />
          <h3>{t("migration.formTitle")}</h3>
        </div>
        <div className="pnl-card-b mig-form">
        <div className="pnl-field">
          <label>{t("migration.provider")}</label>
          <div className="mig-providers">
            {PROVIDERS.map((p) => (
              <button key={p.id} type="button"
                className={"mig-prov" + (provider.id === p.id ? " on" : "")}
                onClick={() => pick(p.id)}>
                {t(`migration.providers.${p.id}`)}
              </button>
            ))}
          </div>
          {/* Thunderbird and Outlook are programs, not providers: what gets imported is the account
              they were set up with. Said here because the customer thinks in terms of the program. */}
          <div className="pnl-note">{t("migration.providerHint")}</div>
        </div>

        <div className="pnl-field">
          <label>{t("migration.username")}</label>
          <input className="pnl-input" value={src.username} autoComplete="off" spellCheck={false}
            placeholder={t("migration.usernamePlaceholder")}
            onChange={(e) => onAddress(e.target.value.trim())} />
        </div>

        {/* Only asked when it is not already known. A field pre-filled with imap.gmail.com is one
            more thing to read and get wrong. */}
        {provider.id === "other" && (
          <div className="pnl-field">
            <label>{t("migration.host")}</label>
            <input className="pnl-input" value={src.host} autoComplete="off" spellCheck={false}
              placeholder={t("migration.hostPlaceholder")}
              onChange={(e) => edit({ host: e.target.value.trim() })} />
            <div className="pnl-note">{t("migration.hostHint")}</div>
          </div>
        )}

        {/* The warning goes ABOVE the password field, not below it: two of these providers refuse an
            ordinary password, and reading that after typing one is reading it too late. */}
        {provider.notice && (
          <p className={"mig-notice" + (provider.notice === "oauthOnly" ? " hard" : "")}>
            {t(`migration.notice.${provider.notice}`)}
          </p>
        )}

        <div className="pnl-field">
          <label>{t("migration.password")}</label>
          <input className="pnl-input" type="password" value={src.password} autoComplete="new-password"
            onChange={(e) => edit({ password: e.target.value })} />
        </div>

        {provider.id === "other" && (
          <label className="mig-check">
            <input type="checkbox" checked={!!src.starttls} onChange={(e) => edit({ starttls: e.target.checked })} />
            <span>{t("migration.starttls")}</span>
          </label>
        )}

        {err && <p className="pnl-err">{err}</p>}

        <div className="mig-actions">
          <button className="pnl-btn" disabled={!ready || check.isPending} onClick={() => check.mutate()}>
            {check.isPending ? t("migration.checking") : t("migration.check")}
          </button>
          {found && (
            <button className="pnl-btn primary" disabled={start.isPending} onClick={() => start.mutate()}>
              <IcDownload size={15} /> {t("migration.start")}
            </button>
          )}
        </div>
        </div>
      </div>
      )}

      {source === "server" && found && <FoundFolders found={found} />}
    </>
  );
}

// FoundFolders is the promise the migration is making, in full: what comes, what does not, and how
// many messages. Shown BEFORE anything is copied.
function FoundFolders({ found }: { found: MigrationCheck }) {
  const { t, i18n } = useTranslation();
  const n = new Intl.NumberFormat(i18n.language);
  const copied = found.folders.filter((f) => !f.skip_reason);
  const skipped = found.folders.filter((f) => f.skip_reason);

  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <IcCheck size={16} />
        <h3>{t("migration.foundTitle", { total: n.format(found.total_messages) })}</h3>
      </div>
      <div className="pnl-card-b mig-found">
      <ul className="mig-folders">
        {copied.map((f) => (
          <li key={f.remote_name}>
            <IcCheck size={14} />
            <span className="name">{f.remote_name}</span>
            <span className="arrow">→</span>
            <span className="dest">{destName(f, t)}</span>
            <span className="count">{n.format(f.messages ?? 0)}</span>
          </li>
        ))}
      </ul>

      {skipped.length > 0 && (
        <>
          <h3 className="mig-skip-h">{t("migration.skippedTitle")}</h3>
          <ul className="mig-folders skipped">
            {skipped.map((f) => (
              <li key={f.remote_name}>
                <IcX size={14} />
                <span className="name">{f.remote_name}</span>
                <span className="why">{t(`migration.skip.${f.skip_reason}`)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
      </div>
    </div>
  );
}

// ---- The job, while it runs ----

function MigrationProgress({ job }: { job: Migration }) {
  const { t, i18n } = useTranslation();
  const qc = useQueryClient();
  const n = new Intl.NumberFormat(i18n.language);
  const done = job.copied_messages + job.skipped_messages + job.failed_messages;
  const pct = job.total_messages > 0 ? Math.min(100, Math.floor((done / job.total_messages) * 100)) : 0;
  // The folder being worked on right now. It is the answer to "is this thing moving", which a
  // percentage that changes every few minutes on a large mailbox does not give on its own.
  const active = job.folders?.find((f) => f.status === "running");

  const cancel = useMutation({
    mutationFn: () => api.migrationCancel(job.id),
    onSuccess: () => { qc.invalidateQueries({ queryKey: MIGRATION_QUERY_KEY }); toast(t("migration.cancelled")); },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="pnl-card">
      <div className="pnl-card-h">
        <IcDownload size={16} />
        <h3>{t(job.status === "pending" ? "migration.queued" : "migration.running")}</h3>
        <div className="grow" />
        {/* The percentage rides in the card header, where every other panel screen puts its state
            pill: it is the one number somebody opens this page to read. */}
        {job.total_messages > 0 && <span className="pnl-pill ok">{pct}%</span>}
      </div>
      <div className="pnl-card-b mig-progress">
      <div className="mig-src">{sourceLabel(job)}</div>

      {/* The folder being copied right now. On a large mailbox the percentage moves every few
          minutes, and on its own it does not answer "is this alive?". */}
      {active && <div className="mig-state">{t("migration.folderNow", { folder: active.remote_name })}</div>}

      <div className={"mig-bar" + (job.total_messages > 0 ? "" : " unknown")}>
        <span style={job.total_messages > 0 ? { width: `${pct}%` } : undefined} />
      </div>

      <div className="mig-counts">
        <b>{n.format(job.copied_messages)}</b> {t("migration.copied")}
        {job.total_messages > 0 && <> {t("migration.ofTotal", { total: n.format(job.total_messages) })}</>}
        {job.skipped_messages > 0 && <> · {n.format(job.skipped_messages)} {t("migration.alreadyHere")}</>}
        {job.failed_messages > 0 && <> · <span className="warn">{n.format(job.failed_messages)} {t("migration.failed")}</span></>}
      </div>

      {job.folders && job.folders.length > 0 && <FolderProgress folders={job.folders} />}

      {/* Cancelling keeps what already arrived. Deleting mail we just copied, on the way out, is not
          a cancellation -- and the button has to say so or nobody will press it. */}
      <p className="pnl-note">{t("migration.cancelKeeps")}</p>
      <button className="pnl-btn ghost" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
        {t("migration.cancel")}
      </button>
      </div>
    </div>
  );
}

function FolderProgress({ folders }: { folders: MigrationFolder[] }) {
  const { t, i18n } = useTranslation();
  const n = new Intl.NumberFormat(i18n.language);
  return (
    <ul className="mig-folders live">
      {folders.map((f) => (
        <li key={f.remote_name} className={f.status}>
          <span className="name">{f.remote_name}</span>
          {f.skip_reason
            ? <span className="why">{t(`migration.skip.${f.skip_reason}`)}</span>
            : <>
                <span className="fstate">{t(`migration.folderState.${f.status ?? "pending"}`)}</span>
                <span className="count">{n.format(f.copied_messages ?? 0)} / {n.format(f.total_messages ?? 0)}</span>
              </>}
        </li>
      ))}
    </ul>
  );
}

// ---- What happened last time ----

function LastRun({ job }: { job: Migration }) {
  const { t, i18n } = useTranslation();
  const n = new Intl.NumberFormat(i18n.language);
  const when = job.finished_at
    ? new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" }).format(new Date(job.finished_at))
    : "";
  const bad = job.status === "failed";

  return (
    <div className={"pnl-card" + (bad ? " mig-last bad" : " mig-last")}>
      <div className="pnl-card-h">
        {bad ? <IcX size={16} /> : <IcRefresh size={16} />}
        <h3>{t(`migration.state.${job.status}`)}</h3>
        <div className="grow" />
        {when && <span className="when">{when}</span>}
      </div>
      <div className="pnl-card-b">
      <div className="mig-src">{sourceLabel(job)}</div>
      <div className="mig-counts">
        <b>{n.format(job.copied_messages)}</b> {t("migration.copied")}
        {job.skipped_messages > 0 && <> · {n.format(job.skipped_messages)} {t("migration.alreadyHere")}</>}
        {/* Messages the source would not hand over. Named on screen because a migration that quietly
            loses eleven out of forty thousand is worse than one that says so. */}
        {job.failed_messages > 0 && <> · <span className="warn">{n.format(job.failed_messages)} {t("migration.failed")}</span></>}
      </div>
      {/* The backend sends a key, never a sentence (law 4.4): whatever a foreign IMAP server says is
          in its own language, and it is for the log, not for this screen. */}
      {bad && job.error_code && <p className="pnl-err">{t(`server.${job.error_code}`)}</p>}
      </div>
    </div>
  );
}

// sourceLabel names where the mail is coming from: an account on a server, or a file.
function sourceLabel(job: Migration): string {
  if (job.kind === "file") return job.source_file || "";
  return `${job.source_username} · ${job.source_host}`;
}

// destName is where a folder lands: a role the account already has, or a folder created by name.
function destName(f: MigrationFolder, t: (k: string) => string): string {
  if (f.local_role) return t(`migration.role.${f.local_role}`);
  return f.local_name || f.remote_name;
}
