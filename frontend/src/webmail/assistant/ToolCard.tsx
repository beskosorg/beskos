import { useTranslation } from "react-i18next";
import type { ToolStep } from "./api";

// Card for one tool call: what it did, on what, and the result (§3.3 of the design). Never a
// sentence from the model claiming what it did — the record of the actual call. This is what
// prevents overtrust: whoever reads it sees the action, not the assertion.
export function ToolCard({ step }: { step: ToolStep }) {
  const { t } = useTranslation();
  // The tool's name is an i18n key. Without a translation, the technical name is shown instead of
  // inventing text — it's honest, and it tells whoever reports a problem which tool was involved.
  const label = t(`assistant.tools.${step.tool}`, { defaultValue: step.tool });
  const failed = !!step.error_code;

  return (
    <div className={"as-tool" + (failed ? " failed" : "")}>
      <div className="as-tool-head">
        <span className="as-tool-ico" aria-hidden="true">{failed ? "⚠" : "🔍"}</span>
        <b>{label}</b>
      </div>
      <div className="as-tool-args">{summarizeArgs(step.args)}</div>
      <div className="as-tool-out">
        {failed ? t(`server.${step.error_code}`, { defaultValue: t("assistant.genericError") }) : step.summary}
      </div>
    </div>
  );
}

// summarizeArgs shows the arguments as the model produced them, in short text. Nothing is hidden:
// what the tool received is part of what's being shown.
function summarizeArgs(args: unknown): string {
  if (!args || typeof args !== "object") return "";
  return Object.entries(args as Record<string, unknown>)
    .filter(([, v]) => v !== "" && v !== null && v !== undefined)
    .map(([k, v]) => `${k}: ${String(v)}`)
    .join(" · ");
}
