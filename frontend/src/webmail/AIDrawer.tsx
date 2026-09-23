import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";
import { backend } from "./backend";
import { Spinner, AiThinking } from "./Spinner";

// "Write with AI" drawer: generates a real reply draft (backend.draft) and lets it be used.
export function AIDrawer({
  original, onClose, onUse,
}: {
  original: string;
  onClose: () => void;
  onUse: (text: string) => void;
}) {
  const { t } = useTranslation();
  const [instruction, setInstruction] = useState("");
  const [tone, setTone] = useState("neutro");
  const [draft, setDraft] = useState("");

  const gen = useMutation({
    mutationFn: () => backend.draft(original, instruction || t("webmail.ai.defaultInstruction"), tone),
    onSuccess: (r) => setDraft(r.draft),
  });

  return (
    <div className="drawer-bg open" onMouseDown={onClose}>
      <div className="drawer" onMouseDown={(e) => e.stopPropagation()}>
        <header>✦ {t("webmail.ai.writeWithAI")} <button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <p style={{ color: "var(--muted)", fontSize: 13, marginTop: 0 }}>{t("webmail.ai.describe")}</p>
          <textarea className="prompt" value={instruction} onChange={(e) => setInstruction(e.target.value)}
            placeholder={t("webmail.ai.instructionPlaceholder")} />
          <button className="gen" disabled={gen.isPending} onClick={() => gen.mutate()}>
            {gen.isPending ? <><Spinner size={14} /> {t("webmail.ai.generating")}</> : "✦ " + t("webmail.ai.generate")}
          </button>
          {gen.isPending && <div className="ai-think-row"><AiThinking /></div>}
          {gen.isError && <p className="err">{(gen.error as Error).message}</p>}
          {draft && (
            <>
              <div className="draft"><div className="lbl">✦ {t("webmail.ai.draftGenerated")}</div>{draft}</div>
              <div className="ai-actions" style={{ marginTop: 14 }}>
                <button className="chipbtn" onClick={() => onUse(draft)}>{t("webmail.ai.useDraft")}</button>
                <button className="chipbtn" onClick={() => { setTone("formal"); gen.mutate(); }}>{t("webmail.ai.moreFormal")}</button>
                <button className="chipbtn" onClick={() => { setTone("curto e direto"); gen.mutate(); }}>{t("webmail.ai.shorter")}</button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
