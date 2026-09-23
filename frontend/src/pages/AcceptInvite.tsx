import { useRef, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { PasswordField } from "../webmail/PasswordField";
import { useQuery } from "@tanstack/react-query";

// Public invite-acceptance page: validates the token and creates the panel account.
export function AcceptInvitePage() {
  const { t } = useTranslation();
  const { token = "" } = useParams();
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [pwd, setPwd] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const pwOk = useRef(false); // password acceptable? (the mail server rejects weak ones)

  const invite = useQuery({
    queryKey: ["invite", token],
    queryFn: async () => {
      const res = await fetch(`/api/invites/${token}`);
      if (!res.ok) throw new Error("invalid");
      return res.json() as Promise<{ email: string; role: string }>;
    },
    retry: false,
  });

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(""); setBusy(true);
    try {
      const res = await fetch("/api/invites/accept", {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, name, password: pwd }),
      });
      if (!res.ok) { const b = await res.json().catch(() => ({})); throw new Error(b.error_code || "error"); }
      nav("/app");
    } catch (e) { setErr((e as Error).message); } finally { setBusy(false); }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <form onSubmit={submit} className="w-full max-w-sm bg-white p-8 rounded-xl shadow space-y-4">
        <h1 className="text-2xl font-semibold">{t("invite.title")}</h1>
        {invite.isLoading ? <p className="text-sm text-gray-500">…</p>
          : invite.isError ? <p className="text-sm text-red-600">{t("invite.invalid")}</p>
          : (
            <>
              <p className="text-sm text-gray-500">{t("invite.joining", { email: invite.data!.email })}</p>
              <input className="w-full border rounded px-3 py-2" placeholder={t("invite.name")} value={name} onChange={(e) => setName(e.target.value)} />
              <PasswordField label={t("login.password")} value={pwd} userInputs={[invite.data?.email ?? ""]}
                onChange={setPwd} onStrength={(s) => { pwOk.current = s.ok; }} />
              {err && <p className="text-sm text-red-600">{err}</p>}
              <button disabled={busy || !pwOk.current} className="w-full bg-black text-white rounded py-2 disabled:opacity-50">
                {busy ? "…" : t("invite.accept")}
              </button>
            </>
          )}
      </form>
    </div>
  );
}
