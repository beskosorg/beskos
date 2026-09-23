import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { backend } from "./backend";
import { mediaUrl } from "../apiBase";
import { toast } from "./toast";
import { IcTrash, IcX } from "../panel/icons";

// Management of the workspace's custom emoji: uploads the image via /api/files and links it to a :name:.
// Any member can add/remove it (it's shared team content). The name only allows [a-z0-9_].
export function EmojiManager({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const list = useQuery({ queryKey: ["chat-emojis"], queryFn: () => backend.chatEmojis(), retry: false });
  const refresh = () => qc.invalidateQueries({ queryKey: ["chat-emojis"] });

  const add = useMutation({
    mutationFn: async () => {
      const clean = name.trim().toLowerCase().replace(/[^a-z0-9_]/g, "");
      if (clean.length < 2) throw new Error(t("chat.emojiBadName"));
      if (!file) throw new Error(t("chat.emojiNeedImage"));
      const up = await backend.uploadFile(file);
      return backend.chatCreateEmoji(clean, up.id);
    },
    onSuccess: () => { setName(""); setFile(null); if (fileRef.current) fileRef.current.value = ""; refresh(); toast(t("chat.emojiAdded")); },
    onError: (e) => toast((e as Error).message),
  });
  const del = useMutation({ mutationFn: (id: string) => backend.chatDeleteEmoji(id), onSuccess: refresh, onError: (e) => toast((e as Error).message) });

  const items = list.data ?? [];
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk modal-sm" onMouseDown={(e) => e.stopPropagation()}>
        <header><span>{t("chat.emojiManage")}</span><button className="ib" title={t("common.close")} onClick={onClose}><IcX size={15} /></button></header>
        <div className="tk-body">
          <div className="emoji-add">
            <button type="button" className="emoji-file" onClick={() => fileRef.current?.click()}>
              {file ? <img src={URL.createObjectURL(file)} alt="" /> : "＋"}
            </button>
            <input ref={fileRef} type="file" accept="image/*" hidden onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            <span className="emoji-colon">:</span>
            <input className="pnl-input" placeholder={t("chat.emojiName")} value={name} maxLength={32}
              onChange={(e) => setName(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ""))} />
            <span className="emoji-colon">:</span>
            <button className="add" disabled={add.isPending || !file || name.trim().length < 2} onClick={() => add.mutate()}>{t("chat.emojiAdd")}</button>
          </div>
          <div className="emoji-mlist">
            {items.length === 0 ? <p className="desc">{t("chat.emojiEmpty")}</p> : items.map((e) => (
              <div className="emoji-mrow" key={e.id}>
                <img className="cemoji" src={mediaUrl(`/api/files/${e.file_id}/thumb`)} alt={e.name} />
                <span className="grow">:{e.name}:</span>
                <button className="ib" title={t("chat.emojiRemove")} onClick={() => del.mutate(e.id)}><IcTrash size={14} /></button>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
