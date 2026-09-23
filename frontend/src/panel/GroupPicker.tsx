import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../api";

// Checkbox group picker — a user can be in SEVERAL groups.
// Used when creating the user and when editing their groups afterward.
export function GroupPicker({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const { t } = useTranslation();
  const groups = useQuery({ queryKey: ["groups"], queryFn: api.groups, retry: false });
  const list = groups.data ?? [];
  if (list.length === 0) return <p className="desc">{t("groups.noneYet")}</p>;

  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);

  return (
    <div className="grp-picker">
      {list.map((g) => (
        <label key={g.id} className={"grp-chk" + (value.includes(g.id) ? " on" : "")}>
          <input type="checkbox" checked={value.includes(g.id)} onChange={() => toggle(g.id)} />
          <span>{g.name}</span>
        </label>
      ))}
    </div>
  );
}
