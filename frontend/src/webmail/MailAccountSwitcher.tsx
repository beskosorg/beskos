import type { Dispatch, SetStateAction } from "react";
import { Avatar } from "./Avatar";
import { IcChevron, IcCheck } from "../panel/icons";

// Address (mailbox) switcher in the mail sidebar, extracted from MailView.tsx (law 1.4).
// Only rendered when there are aliases: without them, a picker with a single entry would be noise.
// A BUTTON that opens the list, not an always-open list: in a narrow rail, full addresses stacked
// up all get cut in half ("dfernandes@beskos.c...") and none of them read.

// Address to show in the picker: local part in bold, domain muted -- that is what distinguishes
// mailboxes on the SAME domain without truncating the text halfway.
const addrLabel = (a: string) => (a.includes("@") ? a.slice(0, a.indexOf("@")) : a);
const addrDomain = (a: string) => (a.includes("@") ? a.slice(a.indexOf("@")) : "");

interface MailAccountSwitcherProps {
  aliases: string[];
  primary: string;
  addrFilter: string;
  setAddrFilter: Dispatch<SetStateAction<string>>;
  addrOpen: boolean;
  setAddrOpen: Dispatch<SetStateAction<boolean>>;
  t: (key: string, opts?: Record<string, unknown>) => string;
}

export function MailAccountSwitcher({ aliases, primary, addrFilter, setAddrFilter, addrOpen, setAddrOpen, t }: MailAccountSwitcherProps) {
  if (aliases.length === 0) return null;
  return (
    <div className="mail-accounts">
      <button className={"ma-cur" + (addrOpen ? " open" : "")} onClick={() => setAddrOpen((v) => !v)}>
        {/* "All" has the same symbol as the menu; an account avatar here would say a single mailbox
            is picked when it is not. */}
        {addrFilter
          ? <Avatar name={addrLabel(addrFilter)} email={addrFilter} size={26} className="ma-av" />
          : <span className="ma-all">@</span>}
        <span className="ma-txt">
          <b>{addrFilter ? addrLabel(addrFilter) : t("webmail.accountsAll")}</b>
          <i>{addrFilter ? addrDomain(addrFilter) : t("webmail.accountsAllHint", { n: aliases.length + 1 })}</i>
        </span>
        <IcChevron size={15} className={"ma-chev" + (addrOpen ? " up" : "")} />
      </button>
      {addrOpen && (
        <>
          <div className="menu-backdrop" onClick={() => setAddrOpen(false)} />
          <div className="ma-menu">
            <button className={addrFilter === "" ? "on" : ""} onClick={() => { setAddrFilter(""); setAddrOpen(false); }}>
              <span className="ma-all">@</span>
              <span className="ma-txt"><b>{t("webmail.accountsAll")}</b><i>{t("webmail.accountsAllHint", { n: aliases.length + 1 })}</i></span>
              {addrFilter === "" && <IcCheck size={15} />}
            </button>
            {[primary, ...aliases].filter(Boolean).map((a) => (
              <button key={a} className={addrFilter === a ? "on" : ""} onClick={() => { setAddrFilter(a); setAddrOpen(false); }}>
                <Avatar name={addrLabel(a)} email={a} size={26} className="ma-av" />
                <span className="ma-txt"><b>{addrLabel(a)}</b><i>{addrDomain(a)}</i></span>
                {addrFilter === a && <IcCheck size={15} />}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
