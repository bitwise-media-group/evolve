import { useCallback, useEffect, useMemo, useState } from "preact/hooks";
import type { Dataset } from "./types";
import {
  clearFacet,
  emptySelection,
  filterRows,
  sortRows,
  toggleFacet,
  type FacetKey,
  type Selection,
  type Sort,
  type SortKey,
} from "./filters";
import {
  embeddedSnapshot,
  fetchResults,
  subscribe,
  type ViewState,
} from "./api";
import { downloadSnapshot } from "./snapshot";
import { FilterPanel } from "./components/FilterPanel";
import { CasesTable } from "./components/CasesTable";
import { RollupTable } from "./components/RollupTable";

type Mode = "cases" | "rollup";
type Scheme = "slate" | "default";

const WORDMARK = [
  ["E", "blue"],
  ["V", "green"],
  ["O", "cyan"],
  ["L", "magenta"],
  ["V", "orange"],
  ["E", "purple"],
] as const;

export function App() {
  const snap = useMemo(embeddedSnapshot, []);
  const offline = snap !== null;

  const [dataset, setDataset] = useState<Dataset | null>(snap?.dataset ?? null);
  const [error, setError] = useState<string | null>(null);
  const [selection, setSelection] = useState<Selection>(
    snap?.view?.selection ?? emptySelection(),
  );
  const [sort, setSort] = useState<Sort>(snap?.view?.sort ?? { key: "plugin", dir: "asc" });
  const [mode, setMode] = useState<Mode>(snap?.view?.mode ?? "cases");
  const [scheme, setScheme] = useState<Scheme>(
    (document.documentElement.getAttribute("data-ev-scheme") as Scheme) || "slate",
  );

  // Apply the theme to the root element.
  useEffect(() => {
    document.documentElement.setAttribute("data-ev-scheme", scheme);
  }, [scheme]);

  const load = useCallback(() => {
    fetchResults().then(setDataset).catch((e: Error) => setError(e.message));
  }, []);

  // Live mode: load from the API and refresh on every results-changed event.
  useEffect(() => {
    if (offline) return;
    load();
    return subscribe(load);
  }, [offline, load]);

  const rows = dataset?.rows ?? [];
  const filtered = useMemo(() => filterRows(rows, selection), [rows, selection]);
  const sorted = useMemo(() => sortRows(filtered, sort), [filtered, sort]);

  const onToggle = (key: FacetKey, value: string) =>
    setSelection((s) => toggleFacet(s, key, value));
  const onClear = (key: FacetKey) => setSelection((s) => clearFacet(s, key));
  const onSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === "asc" ? "desc" : "asc" } : { key, dir: "asc" }));

  const onSnapshot = () => {
    if (!dataset) return;
    const view: ViewState = { selection, sort, mode };
    downloadSnapshot(dataset, view).catch((e: Error) => setError(`snapshot: ${e.message}`));
  };

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="wordmark">
            {WORDMARK.map(([ch, color], i) => (
              <span key={i} style={{ color: `var(--ev-${color})` }}>
                {ch}
              </span>
            ))}
          </span>
          <span className="brand-sub">report viewer</span>
        </div>

        <div className="meta">
          {dataset && (
            <>
              <span className="repo">{dataset.repo}</span>
              <span className="dim">·</span>
              <span className="dim">{dataset.toolVersion}</span>
              <span className="dim">·</span>
              <span className="count">
                {filtered.length} of {rows.length} cases
              </span>
            </>
          )}
        </div>

        <div className="controls">
          <div className="toggle" role="group" aria-label="view mode">
            <button className={mode === "cases" ? "on" : ""} onClick={() => setMode("cases")}>
              Cases
            </button>
            <button className={mode === "rollup" ? "on" : ""} onClick={() => setMode("rollup")}>
              Rollup
            </button>
          </div>
          <button
            className="ghost"
            onClick={onSnapshot}
            disabled={offline || !dataset}
            title={offline ? "Already a snapshot" : "Download a self-contained HTML snapshot"}
          >
            Snapshot
          </button>
          <button
            className="ghost icon"
            onClick={() => setScheme((s) => (s === "slate" ? "default" : "slate"))}
            title="Toggle light/dark"
          >
            {scheme === "slate" ? "☀" : "☾"}
          </button>
          <span className={`live ${offline ? "snap" : "on"}`}>{offline ? "snapshot" : "live"}</span>
        </div>
      </header>

      {error && <div className="banner error">{error}</div>}

      <div className="body">
        <FilterPanel rows={rows} selection={selection} onToggle={onToggle} onClear={onClear} />
        <main className="results">
          {!dataset && !error && <div className="loading">Loading results…</div>}
          {dataset && mode === "cases" && (
            <CasesTable rows={sorted} sort={sort} onSort={onSort} />
          )}
          {dataset && mode === "rollup" && <RollupTable rows={filtered} />}
        </main>
      </div>
    </div>
  );
}
