import { useState } from "react";
import type { StorageFile } from "./backend";
import { FileTypeIcon, fileCategory } from "./filetypes";
import { backend } from "./backend";

// Thumb: real thumbnail for images (/thumb endpoint); falls back to the type icon when it is not
// an image or the thumbnail fails (e.g. webp/svg the stdlib cannot decode -> 415).
// Extracted from FilesView.tsx (law 1.4 -- 600 lines).
export function Thumb({ file, size }: { file: StorageFile; size: number }) {
  const [failed, setFailed] = useState(false);
  if (fileCategory(file.name) === "image" && !failed) {
    return <img className="fthumb" src={backend.fileThumbUrl(file.id)} alt="" loading="lazy" onError={() => setFailed(true)} />;
  }
  return <FileTypeIcon name={file.name} size={size} />;
}
