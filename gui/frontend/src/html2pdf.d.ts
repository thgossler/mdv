// Minimal ambient typings for html2pdf.js, which ships no types. Only the
// small chained surface used by the PDF export fallback is declared.
declare module "html2pdf.js" {
  interface Html2PdfWorker {
    set(opts: Record<string, unknown>): Html2PdfWorker;
    from(element: HTMLElement | string): Html2PdfWorker;
    outputPdf(type: "blob"): Promise<Blob>;
    save(): Promise<void>;
  }
  function html2pdf(): Html2PdfWorker;
  export default html2pdf;
}
