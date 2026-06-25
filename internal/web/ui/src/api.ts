// Data access: the embedded snapshot (offline) or the live read-only API.

import type { Dataset } from "./types";
import type { Selection, Sort } from "./filters";

// ViewState is the serialised UI state a snapshot pre-applies on open.
export interface ViewState {
  selection: Selection;
  sort: Sort;
  mode: "cases" | "rollup";
}

export interface Snapshot {
  dataset: Dataset;
  view: ViewState | null;
}

// embeddedSnapshot returns the inlined dataset when the page was opened as a
// snapshot (window.__EVOLVE_SNAPSHOT__), else null (live/served mode).
export function embeddedSnapshot(): Snapshot | null {
  const s = (window as unknown as { __EVOLVE_SNAPSHOT__?: Snapshot }).__EVOLVE_SNAPSHOT__;
  return s && s.dataset ? s : null;
}

export async function fetchResults(): Promise<Dataset> {
  const res = await fetch("/api/results");
  if (!res.ok) throw new Error(`results: HTTP ${res.status}`);
  return (await res.json()) as Dataset;
}

// subscribe opens the SSE stream and calls onChange whenever the results files
// change on disk. Returns an unsubscribe function. Best-effort: EventSource
// retries on its own, and a failure just means no live refresh.
export function subscribe(onChange: () => void): () => void {
  const es = new EventSource("/events");
  es.addEventListener("results-changed", onChange);
  return () => es.close();
}
