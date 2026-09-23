import { useTranslation } from "react-i18next";

// Loading spinner (spinning ring). Used across every AI action and similar ones.
export function Spinner({ size = 15 }: { size?: number }) {
  return <span className="spin" style={{ width: size, height: size }} aria-label="loading" role="status" />;
}

// "AI thinking…" indicator for larger areas (summary card, panels).
export function AiThinking({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <span className="ai-think">
      <Spinner size={14} /> {label ?? t("webmail.ai.thinking")}
    </span>
  );
}
