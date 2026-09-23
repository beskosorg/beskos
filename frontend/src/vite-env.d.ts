// Asset import with the ?url suffix (Vite): returns the bundled file's final URL. Used by
// pdf.js's worker (pdfjs.ts). Without depending on the full vite/client.
declare module "*?url" {
  const src: string;
  export default src;
}
