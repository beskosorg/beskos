import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend } from "./backend";

const PALETTE = ["#B4785F", "#3B6FB0", "#8257C7", "#B44D3A", "#2E7D5B", "#C0692F", "#C9871F"];
const colorFor = (name: string) => PALETTE[[...name].reduce((a, c) => a + c.charCodeAt(0), 0) % PALETTE.length];

// Tag picker from the shared catalog (events, contacts…). Creates new tags in the catalog.
export function TagPicker({ value, onChange }: { value: string[]; onChange: (t: string[]) => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const tags = useQuery({ queryKey: ["tags"], queryFn: () => backend.tagsList(), retry: false });
  const [input, setInput] = useState("");
  const catalog = tags.data ?? [];
  const color = (name: string) => catalog.find((c) => c.name === name)?.color ?? colorFor(name);

  const add = useMutation({
    mutationFn: (name: string) => backend.tagUpsert(name, color(name)),
    onSuccess: (tag) => { qc.invalidateQueries({ queryKey: ["tags"] }); if (!value.includes(tag.name)) onChange([...value, tag.name]); setInput(""); },
  });
  const commit = () => { const n = input.trim(); if (n) add.mutate(n); };
  const q = input.trim().toLowerCase();
  const suggestions = q ? catalog.filter((c) => c.name.toLowerCase().includes(q) && !value.includes(c.name)).slice(0, 6) : [];

  return (
    <div className="tagpick">
      <div className="tp-chips">
        {value.map((name) => (
          <span key={name} className="tp-chip" style={{ background: color(name) + "22", color: color(name), borderColor: color(name) + "55" }}>
            {name}<button title={t("common.remove")} onClick={() => onChange(value.filter((x) => x !== name))}>✕</button>
          </span>
        ))}
        <div className="tp-input">
          <input value={input} placeholder={t("cal.addTag")} onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); commit(); } }} />
          {suggestions.length > 0 && (
            <div className="tp-sugg">
              {suggestions.map((c) => (
                <button key={c.id} onMouseDown={(e) => { e.preventDefault(); if (!value.includes(c.name)) onChange([...value, c.name]); setInput(""); }}>
                  <span className="dot" style={{ background: c.color }} />{c.name}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export { colorFor };
