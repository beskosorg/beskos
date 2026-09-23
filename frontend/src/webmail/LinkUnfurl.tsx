import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { backend } from "./backend";
import { href as linkHref } from "./links";
import { IcColumns, IcPencilBox } from "../panel/icons";

// LinkUnfurl fetches a URL's metadata once (cached) and shows a preview card.
export function LinkUnfurl({ url }: { url: string }) {
  const { t } = useTranslation();
  // The address goes to the API already normalised: a bare host has no scheme, and the server needs
  // one to fetch anything at all.
  const target = linkHref(url);
  const q = useQuery({ queryKey: ["unfurl", target], queryFn: () => backend.chatUnfurl(target), retry: false, staleTime: 3600_000 });
  const d = q.data?.unfurl;
  if (!d) return null;

  // One of ours: opens INSIDE the app. A plain href would reload the whole bundle, and in the
  // desktop build it leaves the app entirely -- the asset server has no SPA fallback.
  if (d.kind === "task" || d.kind === "note") {
    const path = new URL(d.url).pathname;
    return (
      <a className={"unfurl unfurl-" + d.kind} href={path}
        onClick={(e) => { if (!e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0) { e.preventDefault(); window.history.pushState({}, "", path); window.dispatchEvent(new PopStateEvent("popstate")); } }}>
        <div className="unfurl-txt">
          {d.site && <div className="unfurl-site">{d.kind === "note" ? <IcPencilBox size={12} /> : <IcColumns size={12} />} {d.site}</div>}
          <div className="unfurl-title">{d.title || t(d.kind === "note" ? "notes.untitled" : "webmail.kanban.untitled")}</div>
          {d.description && <div className="unfurl-desc">{d.description}</div>}
        </div>
      </a>
    );
  }

  return (
    <a className="unfurl" href={d.url} target="_blank" rel="noopener noreferrer nofollow">
      {d.image && <img className="unfurl-img" src={d.image} alt="" loading="lazy" onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = "none"; }} />}
      <div className="unfurl-txt">
        {d.site && <div className="unfurl-site">{d.site}</div>}
        <div className="unfurl-title">{d.title}</div>
        {d.description && <div className="unfurl-desc">{d.description}</div>}
      </div>
    </a>
  );
}
