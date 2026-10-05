import {
  getDocument,
  GlobalWorkerOptions,
} from "pdfjs-dist/legacy/build/pdf.mjs";
import workerURL from "pdfjs-dist/legacy/build/pdf.worker.min.mjs?url";

// This entry parses a fixed, staged PDF. It never invokes PDF JavaScript,
// opens document links, renders annotations, or requests a URL from PDF content.
type DocumentStatus = {
  ready: boolean;
  pages?: number;
  text?: string;
  error?: string;
};
declare global {
  interface Window {
    troveDocumentStatus: DocumentStatus;
  }
}
window.troveDocumentStatus = { ready: false };
GlobalWorkerOptions.workerSrc = workerURL;
const countOnly = location.pathname === "/validate.html";
const maximumBytes = (countOnly ? 256 : 128) << 20;
const maximumPages = countOnly ? 2000 : 10000;
const maximumText = 16 << 20;
const encoder = new TextEncoder();
const abort = new AbortController();
const deadline = setTimeout(() => abort.abort(), 95_000);
window.addEventListener("pagehide", () => abort.abort(), { once: true });
async function readPDF() {
  const response = await fetch(countOnly ? "/result.pdf" : "/input.pdf", {
    signal: abort.signal,
    credentials: "omit",
    redirect: "error",
    cache: "no-store",
  });
  if (!response.ok || !response.body)
    throw new Error("Staged PDF is unavailable");
  const reader = response.body.getReader(),
    chunks: Uint8Array[] = [];
  let length = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.length;
      if (length > maximumBytes) throw new Error("PDF exceeds input bound");
      chunks.push(value);
    }
  } catch (error) {
    await reader.cancel();
    throw error;
  }
  const data = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    data.set(chunk, offset);
    offset += chunk.length;
  }
  if (data.length < 5 || String.fromCharCode(...data.slice(0, 5)) !== "%PDF-")
    throw new Error("Invalid PDF header");
  return data;
}
async function extract() {
  const task = getDocument({
    data: await readPDF(),
    enableXfa: false,
    disableAutoFetch: true,
    disableStream: true,
    useWorkerFetch: false,
    useSystemFonts: false,
    stopAtErrors: true,
    isOffscreenCanvasSupported: false,
    maxImageSize: 20_000_000,
    cMapUrl: "/pdfjs-assets/cmaps/",
    cMapPacked: true,
    standardFontDataUrl: "/pdfjs-assets/standard_fonts/",
    wasmUrl: "/pdfjs-assets/wasm/",
    iccUrl: "/pdfjs-assets/iccs/",
  });
  // Password-protected input is an explicit unsupported case, with no prompt.
  task.onPassword = () => {
    void task.destroy();
  };
  const onAbort = () => {
    void task.destroy();
  };
  abort.signal.addEventListener("abort", onAbort, { once: true });
  let result: DocumentStatus;
  try {
    const pdf = await task.promise;
    if (abort.signal.aborted) throw new Error("PDF processing cancelled");
    if (pdf.numPages < 1 || pdf.numPages > maximumPages)
      throw new Error("PDF exceeds page bound");
    const chunks: string[] = [];
    let length = 0;
    const append = (value: string) => {
      length += encoder.encode(value).length;
      if (length > maximumText) throw new Error("PDF text exceeds 16 MiB");
      chunks.push(value);
    };
    if (!countOnly) {
      for (let n = 1; n <= pdf.numPages; n++) {
        if (abort.signal.aborted) throw new Error("PDF processing cancelled");
        const page = await pdf.getPage(n);
        try {
          const content = await page.getTextContent({
            includeMarkedContent: false,
          });
          for (const item of content.items) {
            if ("str" in item) append(item.str + (item.hasEOL ? "\n" : " "));
          }
        } finally {
          page.cleanup();
        }
        if (n < pdf.numPages) append("\n\f\n");
      }
    }
    result = {
      ready: true,
      pages: pdf.numPages,
      ...(!countOnly ? { text: chunks.join("") } : {}),
    };
  } finally {
    abort.signal.removeEventListener("abort", onAbort);
    await task.destroy();
  }
  if (abort.signal.aborted) throw new Error("PDF processing cancelled");
  window.troveDocumentStatus = result;
}
void extract()
  .catch(() => {
    window.troveDocumentStatus = {
      ready: false,
      error:
        "PDF parsing failed, was cancelled, or exceeded the declared input/page/text limits.",
    };
  })
  .finally(() => clearTimeout(deadline));
