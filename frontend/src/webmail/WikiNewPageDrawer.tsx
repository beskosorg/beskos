import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import { wikiApi } from "./backend";
import { toast } from "./toast";
import { Drawer } from "./Drawer";
import { IcPencilBox } from "../panel/icons";

// WikiNewPageDrawer: create a page by choosing a TEMPLATE — blank or one of the built-in templates (with
// a CSS-drawn thumbnail representing the structure), plus the space's own templates
// (pages marked as a template). The templates' content is 100% i18n.

type TFn = (k: string) => string;
const bnText = (text: string) => [{ type: "text", text, styles: {} }];
const h2 = (t: string) => ({ type: "heading", props: { level: 2 }, content: bnText(t), children: [] });
const para = (t: string) => ({ type: "paragraph", props: {}, content: bnText(t), children: [] });
const bullet = (t: string) => ({ type: "bulletListItem", props: {}, content: bnText(t), children: [] });
const num = (t: string) => ({ type: "numberedListItem", props: {}, content: bnText(t), children: [] });
const check = (t: string) => ({ type: "checkListItem", props: {}, content: bnText(t), children: [] });

// Thumbnail: sequence of strokes representing the template's structure (h=heading, p=text, b=bullet,
// n=numbered, c=checkbox).
type Tpl = { key: string; thumb: string[]; blocks: (t: TFn) => unknown[] };
const BUILTIN_TEMPLATES: Tpl[] = [
  { key: "blank", thumb: ["p"], blocks: () => [{ type: "paragraph", props: {}, content: [], children: [] }] },
  {
    key: "basic", thumb: ["h", "p", "h", "b", "b", "h", "p"],
    blocks: (t) => [
      h2(t("wiki.tpl.overview")), para(t("wiki.tpl.overviewHint")),
      h2(t("wiki.tpl.details")), bullet(t("wiki.tpl.point1")), bullet(t("wiki.tpl.point2")),
      h2(t("wiki.tpl.notes")), para(t("wiki.tpl.notesHint")),
    ],
  },
  {
    key: "meeting", thumb: ["h", "b", "b", "h", "b", "h", "c", "c"],
    blocks: (t) => [
      h2(t("wiki.tplm.participants")), bullet(t("wiki.tplm.person")),
      h2(t("wiki.tplm.agenda")), bullet(t("wiki.tplm.topic")),
      h2(t("wiki.tplm.decisions")), bullet(t("wiki.tplm.decision")),
      h2(t("wiki.tplm.actions")), check(t("wiki.tplm.action1")), check(t("wiki.tplm.action2")),
    ],
  },
  {
    key: "project", thumb: ["h", "p", "h", "b", "b", "h", "b", "h", "p"],
    blocks: (t) => [
      h2(t("wiki.tplm.goal")), para(t("wiki.tplm.goalHint")),
      h2(t("wiki.tplm.scope")), bullet(t("wiki.tplm.inScope")), bullet(t("wiki.tplm.outScope")),
      h2(t("wiki.tplm.risks")), bullet(t("wiki.tplm.risk")),
      h2(t("wiki.tplm.timeline")), para(t("wiki.tplm.timelineHint")),
    ],
  },
  {
    key: "howto", thumb: ["h", "p", "h", "n", "n", "n", "h", "p"],
    blocks: (t) => [
      h2(t("wiki.tplm.when")), para(t("wiki.tplm.whenHint")),
      h2(t("wiki.tplm.steps")), num(t("wiki.tplm.step1")), num(t("wiki.tplm.step2")), num(t("wiki.tplm.step3")),
      h2(t("wiki.tplm.outcome")), para(t("wiki.tplm.outcomeHint")),
    ],
  },
];

function TplThumb({ pattern }: { pattern: string[] }) {
  return (
    <div className="tplthumb">
      {pattern.map((k, i) => (
        <div key={i} className={"tplthumb-" + k}>
          {(k === "b" || k === "n" || k === "c") && <span className="tick" />}
          <span className="bar" />
        </div>
      ))}
    </div>
  );
}

export function WikiNewPageDrawer({ spaceId, onClose, onCreated }: {
  spaceId: string; onClose: () => void; onCreated: (pageId: string) => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [picked, setPicked] = useState("basic"); // "blank" | builtin key | "space:<pageId>"

  const spaceTpls = useQuery({ queryKey: ["wiki-templates", spaceId], queryFn: () => wikiApi.templates(spaceId), retry: false });

  const create = useMutation({
    mutationFn: async () => {
      let body: unknown;
      if (picked.startsWith("space:")) {
        body = (await wikiApi.getPage(picked.slice(6))).page.body_json ?? [];
      } else {
        const tpl = BUILTIN_TEMPLATES.find((x) => x.key === picked) ?? BUILTIN_TEMPLATES[0];
        body = tpl.blocks(t);
      }
      return wikiApi.createPage(spaceId, { title: title.trim(), body_json: body });
    },
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["wiki-tree", spaceId] });
      toast(t("wiki.created")); onCreated(p.id); onClose();
    },
    onError: (e) => toast((e as Error).message),
  });

  return (
    <Drawer wide title={t("wiki.newPage")} onClose={onClose}
      footer={<>
        <button className="chipbtn" onClick={onClose}>{t("wiki.cancel")}</button>
        <div className="grow" />
        <button className="add" disabled={!title.trim() || create.isPending} onClick={() => create.mutate()}>{t("wiki.create")}</button>
      </>}>
      <label className="wiki-field"><span>{t("wiki.pageTitle")}</span>
        <input autoFocus value={title} onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && title.trim()) create.mutate(); }} /></label>

      <div className="wiki-side-title">{t("wiki.tplGallery.pick")}</div>
      <div className="tpl-grid">
        {BUILTIN_TEMPLATES.map((tpl) => (
          <button key={tpl.key} className={"tpl-card" + (picked === tpl.key ? " on" : "")} onClick={() => setPicked(tpl.key)}>
            <TplThumb pattern={tpl.thumb} />
            <span className="tpl-name">{t(`wiki.tplGallery.${tpl.key}`)}</span>
          </button>
        ))}
        {(spaceTpls.data ?? []).map((sp) => (
          <button key={sp.id} className={"tpl-card" + (picked === "space:" + sp.id ? " on" : "")} onClick={() => setPicked("space:" + sp.id)}>
            <div className="tplthumb tplthumb-custom"><IcPencilBox size={22} /></div>
            <span className="tpl-name">{sp.title}</span>
          </button>
        ))}
      </div>
    </Drawer>
  );
}
