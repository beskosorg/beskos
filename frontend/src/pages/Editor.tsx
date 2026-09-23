import "../webmail/webmail.css";
import { useWebmail } from "../webmail/auth";
import { WebmailLogin } from "../webmail/WebmailLogin";
import { EditorView } from "../webmail/EditorView";
import { ToastHost } from "../webmail/toast";

// Route /mail/files/edit/:id -- full-screen office editor (Collabora/WOPI). Requires a webmail
// session; without one, it falls back to login.
export function EditorPage() {
  const { session } = useWebmail();
  if (!session) return <WebmailLogin />;
  return (
    <div className="wm">
      <EditorView />
      <ToastHost />
    </div>
  );
}
