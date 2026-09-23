import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { BlockNoteSchema, defaultInlineContentSpecs, filterSuggestionItems, PartialBlock } from "@blocknote/core";
import * as locales from "@blocknote/core/locales";
import { BlockNoteView } from "@blocknote/mantine";
import {
  useCreateBlockNote, SuggestionMenuController, getDefaultReactSlashMenuItems,
  createReactInlineContentSpec, type DefaultReactSuggestionItem,
} from "@blocknote/react";
import * as Y from "@y/y";
import "@blocknote/core/fonts/inter.css";
import "@blocknote/mantine/style.css";

import { backend, colleaguesApi } from "./backend";
import { apiBase } from "../apiBase";
import { toast } from "./toast";
import { WikiCollabProvider } from "./WikiCollabProvider";
import { IcSparkle } from "../panel/icons";

// WikiEditor is the wiki's rich editor — BlockNote (core MPL-2.0; B2). body_json is the ARRAY of blocks
// (source of truth); the backend derives body_text/body_html. AI comes in via custom slash-menu items
// (our own /api/ai/wiki-write — never the AGPL xl-ai package). With pageId, it wires up real-time
// COLLABORATION (B10): Y.Doc + Go relay; peers===0 on the hello → seeds from body_json; otherwise syncs from the peers.

function textToBlocks(text: string): PartialBlock[] {
  const paras = text.split(/\n+/).map((p) => p.trim()).filter(Boolean);
  if (paras.length === 0) return [{ type: "paragraph" }];
  return paras.map((p) => ({ type: "paragraph", content: p }));
}

// Stable color per name for the collaborative cursor.
const CURSOR_COLORS = ["#e07a5f", "#3d84a8", "#81b29a", "#f2a154", "#9b5de5", "#00afb9", "#ef476f", "#118ab2"];
function colorFor(name: string): string {
  let h = 0;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) | 0;
  return CURSOR_COLORS[Math.abs(h) % CURSOR_COLORS.length];
}

// @person mentions and [[page]] links (B2 leftover): custom inline content serialized as
// {type:"mention"|"pageLink", props:{id,label}} — render_blocknote.go handles both with escaping.
const MentionSpec = createReactInlineContentSpec(
  { type: "mention", propSchema: { id: { default: "" }, label: { default: "" } }, content: "none" },
  { render: (p) => <span className="wiki-mention">@{p.inlineContent.props.label}</span> },
);
const PageLinkSpec = createReactInlineContentSpec(
  { type: "pageLink", propSchema: { id: { default: "" }, label: { default: "" } }, content: "none" },
  { render: (p) => <span className="wiki-pagelink">{p.inlineContent.props.label}</span> },
);
const wikiSchema = BlockNoteSchema.create({
  inlineContentSpecs: { ...defaultInlineContentSpecs, mention: MentionSpec, pageLink: PageLinkSpec },
});

export function WikiEditor({ value, bodyText, onChange, pageId, pages }: {
  value: unknown;                 // current body_json (BlockNote array, or legacy)
  bodyText?: string;              // plain text, to seed/migrate legacy content
  onChange: (json: unknown) => void;
  pageId?: string;                // with pageId, wires up real-time collaboration (B10)
  pages?: { id: string; label: string }[]; // space's pages for the [[page]] picker
}) {
  const { t, i18n } = useTranslation();
  const me = useQuery({ queryKey: ["me"], queryFn: () => backend.me(), retry: false, staleTime: 60_000 });
  const colleagues = useQuery({ queryKey: ["colleagues"], queryFn: () => colleaguesApi.list(), retry: false, staleTime: 60_000 });

  // Doc + provider created ONCE per mount (WikiPagePane mounts the editor keyed by page).
  const [collab] = useState(() => {
    if (!pageId) return null;
    const doc = new Y.Doc();
    return { doc, provider: new WikiCollabProvider(pageId, doc) };
  });
  useEffect(() => {
    // StrictMode (dev) unmounts and remounts: the provider is shared via state, so it is revived
    // on mount instead of creating another one — and destroyed on the real cleanup.
    collab?.provider.ensureConnected();
    return () => collab?.provider.scheduleDestroy();
  }, [collab]);

  const seedBlocks = (): PartialBlock[] => {
    let v: unknown = value;
    // Defense: old responses (pre-fix) carried body_json as a base64/JSON string — try to parse it.
    if (typeof v === "string") {
      const raw: string = v;
      try { v = JSON.parse(raw); } catch { try { v = JSON.parse(atob(raw)); } catch { v = null; } }
    }
    if (Array.isArray(v) && v.length > 0) return v as PartialBlock[];
    return textToBlocks(bodyText ?? "");
  };

  const dictionary = i18n.language.toLowerCase().startsWith("pt") ? locales.pt : locales.en;
  // Images/files inserted into the page: upload to DRIVE + a public link for the file (the page's
  // reader — including the public route — needs a reachable URL). The warning that these links
  // are public lives in the public-access toggle (WikiShareDrawer).
  const uploadFile = async (file: File): Promise<string> => {
    const sf = await backend.uploadFile(file);
    const link = await backend.fileLink(sf.id, { allow_download: true });
    return `${apiBase() || ""}/api/public/files/${link.token}/download?inline=1`;
  };
  const editor = useCreateBlockNote(
    collab
      ? {
          schema: wikiSchema,
          dictionary,
          uploadFile,
          collaboration: {
            provider: collab.provider,
            fragment: collab.doc.get("document-store") as never, // v14 RC: get(name) returns the shared type
            user: { name: "…", color: CURSOR_COLORS[0] },
          },
        }
      : { schema: wikiSchema, dictionary, uploadFile, initialContent: seedBlocks() },
  );

  // Seed rule (B10): only the FIRST one in the room loads the body_json; the rest sync up.
  // Safety net: if the hello doesn't arrive (WS blocked/offline), seed anyway after a
  // moment — editing alone must never open blank; the optimistic 409 protects the rest.
  const seeded = useRef(false);
  useEffect(() => {
    if (!collab) return;
    const docEmpty = () => editor.document.length === 0 ||
      (editor.document.length === 1 && !(editor.document[0] as { content?: unknown[] }).content?.length);
    const seed = () => {
      if (seeded.current) return;
      seeded.current = true;
      if (docEmpty()) editor.replaceBlocks(editor.document, seedBlocks());
    };
    collab.provider.onHello = (peers) => {
      if (peers === 0) seed();
      // peers>0: the content comes from syncing with peers; the fallback below covers phantom connections.
    };
    // 600ms: the local hello arrives in ~10ms; if the WS is down, the content can't wait.
    const fallback = setTimeout(() => { if (docEmpty()) seed(); }, 600);
    return () => clearTimeout(fallback);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [collab, editor]);

  // The cursor's real name/color arrive later (query me): updates via awareness (BlockNote reads from there).
  useEffect(() => {
    if (!collab || !me.data) return;
    const name = me.data.name || me.data.email || "?";
    collab.provider.awareness.setLocalStateField("user", { name, color: colorFor(name) });
  }, [collab, me.data]);

  async function runAI(instruction: string) {
    toast(t("wiki.ai.working"));
    try {
      const context = await editor.blocksToMarkdownLossy(editor.document);
      const res = await backend.aiWikiWrite(instruction, context);
      const blocks = await editor.tryParseMarkdownToBlocks(res.content);
      editor.insertBlocks(blocks, editor.getTextCursorPosition().block, "after");
    } catch (e) {
      toast((e as Error).message);
    }
  }

  const aiItems: DefaultReactSuggestionItem[] = [
    {
      title: t("wiki.ai.continue"), subtext: t("wiki.ai.continueHint"), group: t("wiki.ai.group"),
      icon: <IcSparkle size={18} />, aliases: ["ai", "ia", "continuar"],
      onItemClick: () => void runAI("Continue writing this wiki page from where it stops, keeping the same tone and structure."),
    },
    {
      title: t("wiki.ai.improve"), subtext: t("wiki.ai.improveHint"), group: t("wiki.ai.group"),
      icon: <IcSparkle size={18} />, aliases: ["ai", "ia", "melhorar"],
      onItemClick: () => void runAI("Rewrite the page content improving clarity and structure, keeping the meaning. Return the full improved content."),
    },
    {
      title: t("wiki.ai.summarize"), subtext: t("wiki.ai.summarizeHint"), group: t("wiki.ai.group"),
      icon: <IcSparkle size={18} />, aliases: ["ai", "ia", "resumo"],
      onItemClick: () => void runAI("Write a concise summary (a few bullet points) of this page."),
    },
  ];

  const mentionItems = (): DefaultReactSuggestionItem[] =>
    (colleagues.data ?? []).map((c) => ({
      title: c.name || c.email, subtext: c.email,
      onItemClick: () => editor.insertInlineContent([{ type: "mention", props: { id: c.id, label: c.name || c.email } }, " "]),
    }));
  const pageItems = (): DefaultReactSuggestionItem[] =>
    (pages ?? []).map((pg) => ({
      title: pg.label,
      onItemClick: () => editor.insertInlineContent([{ type: "pageLink", props: { id: pg.id, label: pg.label } }, " "]),
    }));

  const theme = document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";

  return (
    <div className="wiki-editor">
      <BlockNoteView editor={editor} theme={theme} slashMenu={false}
        onChange={() => onChange(editor.document)}>
        <SuggestionMenuController triggerCharacter="/"
          getItems={async (query) =>
            filterSuggestionItems([...getDefaultReactSlashMenuItems(editor), ...aiItems], query)} />
        <SuggestionMenuController triggerCharacter="@"
          getItems={async (query) => filterSuggestionItems(mentionItems(), query)} />
        <SuggestionMenuController triggerCharacter="["
          getItems={async (query) => filterSuggestionItems(pageItems(), query)} />
      </BlockNoteView>
    </div>
  );
}
