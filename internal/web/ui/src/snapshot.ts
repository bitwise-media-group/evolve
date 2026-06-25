// Client-side snapshot export: fetch the pristine single-file shell, inline the
// current dataset + view as window.__EVOLVE_SNAPSHOT__, and download it. The
// result is fully self-contained (the build is one inlined index.html) and opens
// offline over file://, with the captured filters/sort pre-applied.

import type { Dataset } from "./types";
import type { ViewState } from "./api";

// injectSnapshot inserts the data script at the top of <head> so it runs before
// the deferred module bundle. "<" is escaped so payload data containing
// "</script>" cannot terminate the element (mirrors the Go injector).
export function injectSnapshot(shell: string, dataset: Dataset, view: ViewState): string {
  const payload = JSON.stringify({ dataset, view }).replace(/</g, "\\u003c");
  const script = `<script>window.__EVOLVE_SNAPSHOT__ = ${payload};</script>`;
  const open = shell.indexOf("<head>");
  if (open >= 0) {
    const at = open + "<head>".length;
    return shell.slice(0, at) + script + shell.slice(at);
  }
  const close = shell.indexOf("</head>");
  if (close >= 0) return shell.slice(0, close) + script + shell.slice(close);
  throw new Error("snapshot: no <head> in the page shell");
}

export async function downloadSnapshot(dataset: Dataset, view: ViewState): Promise<void> {
  // The served page's "/" is the pristine single-file shell to clone into.
  const res = await fetch("/");
  if (!res.ok) throw new Error(`snapshot: HTTP ${res.status}`);
  const shell = await res.text();
  const html = injectSnapshot(shell, dataset, view);

  const blob = new Blob([html], { type: "text/html" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `evolve-${dataset.repo || "report"}-snapshot.html`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
