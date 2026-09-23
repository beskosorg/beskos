import "../webmail/webmail.css";
import { Navigate, useParams } from "react-router-dom";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { App } from "../webmail/App";

// Route /mail: the end user's email app. Login validates against Stalwart (JMAP) and also opens
// a session with our own API (AI/Kanban/calendar).
export function MailPage() {
  const { session } = useWebmail();
  const { view, sub } = useParams<{ view: string; sub: string }>();
  if (session) return <App session={session} />;
  // /mail/meet/<room> is the URL a member sees while in a call, so it is what gets shared. For
  // someone without a session it is the meeting's guest link, not a reason to ask for a login.
  if (view === "meet" && sub && sub !== "m") return <Navigate to={`/j/${encodeURIComponent(sub)}`} replace />;
  return <WebmailLogin />;
}
