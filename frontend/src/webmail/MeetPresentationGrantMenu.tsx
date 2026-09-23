import { useState } from "react";
import { IcUsers } from "../panel/icons";
import { hueFor, initials } from "./util";

// Menu to grant/revoke pen access to participants. Extracted from MeetPresentation.tsx (law 1.4).
export function GrantMenu({ peers, granted, onToggle, label }: {
  peers: { identity: string; name?: string }[]; granted: Set<string>; onToggle: (id: string) => void; label: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="pres-grant">
      <button className={"ib" + (open ? " on" : "")} title={label} onClick={() => setOpen((o) => !o)}><IcUsers size={15} /></button>
      {open && (
        <>
          <div className="pres-grant-back" onClick={() => setOpen(false)} />
          <div className="pres-grant-menu">
            <b>{label}</b>
            {peers.map((p) => {
              const on = granted.has(p.identity);
              const nm = p.name || p.identity;
              return (
                <button key={p.identity} type="button" className="pres-grant-row" onClick={() => onToggle(p.identity)}>
                  <span className="pg-av" style={{ background: hueFor(p.identity) }}>{initials(nm)}</span>
                  <span className="pg-nm">{nm}</span>
                  <span className={"pg-sw" + (on ? " on" : "")}><i /></span>
                </button>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
