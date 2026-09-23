import { useState, useRef } from "react";

// Drag-and-drop of files onto any region. Shared by the chat channel and the mail composer, which
// is why it does not live next to either -- e-mail has no business importing chat internals.
//
// Counts enter/leave instead of trusting a single flag: moving the pointer over a CHILD element
// fires dragleave on the parent, and a naive implementation flickers the drop overlay on and off
// while the file is still very much over the target.
//
// Only reacts to a drag carrying FILES. Dragging a selection, a link or a card inside the app also
// raises drag events, and lighting up "drop your files here" for those is noise.
// Drag-and-drop zone for files. Only reacts to a drag of files (ignores drag of other
// elements); uses an enter/leave counter to avoid flickering when passing over children.
export function useFileDrop(onDrop: (files: FileList) => void) {
  const [over, setOver] = useState(false);
  const depth = useRef(0);
  const hasFiles = (e: React.DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes("Files");
  const handlers = {
    onDragEnter: (e: React.DragEvent) => { if (!hasFiles(e)) return; e.preventDefault(); depth.current++; setOver(true); },
    onDragOver: (e: React.DragEvent) => { if (!hasFiles(e)) return; e.preventDefault(); e.dataTransfer.dropEffect = "copy"; },
    onDragLeave: (e: React.DragEvent) => { if (!hasFiles(e)) return; depth.current--; if (depth.current <= 0) { depth.current = 0; setOver(false); } },
    onDrop: (e: React.DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault(); depth.current = 0; setOver(false);
      if (e.dataTransfer.files?.length) onDrop(e.dataTransfer.files);
    },
  };
  return { over, handlers };
}
