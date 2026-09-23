// pdf.js setup. The worker is bundled by Vite (?url) — works the same on the web and in the
// native app (Tauri), without depending on a CDN. worker-src 'self' blob: in the CSP covers it.
import * as pdfjsLib from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";

pdfjsLib.GlobalWorkerOptions.workerSrc = workerUrl;

export type PDFDocument = Awaited<ReturnType<typeof pdfjsLib.getDocument>["promise"]>;

// loadPdf opens a PDF served by our API. Two attempts, in this order:
//
//  1. Normal (streaming + Range): the presentation endpoint responds 206, so the first page
//     appears after a few KB instead of waiting for the whole file — decisive for a multi-MB deck,
//     where the other participants were left staring at a blank rectangle.
//  2. Whole file (disableRange/disableStream): if something in the chain (proxy, cache) breaks
//     range requests — returning a full 200 to a Range request, for example — pdf.js fails; so we
//     download it all at once, which is the path that works on any server.
export async function loadPdf(url: string): Promise<PDFDocument> {
  try {
    // 512 KB chunks (the default is 64 KB): a multi-MB deck goes from ~100 requests to far fewer,
    // which fits within the public endpoint's per-IP limit without sacrificing fast startup.
    return await pdfjsLib.getDocument({ url, rangeChunkSize: 512 * 1024 }).promise;
  } catch (e) {
    console.warn("pdf: range read failed, downloading the whole file", e);
    return await pdfjsLib.getDocument({ url, disableRange: true, disableStream: true }).promise;
  }
}
